/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/patrickmn/go-cache"

	"github.com/aws/karpenter/pkg/apis/provisioning/v1alpha5"
	"github.com/aws/karpenter/pkg/cloudprovider"
	"github.com/aws/karpenter/pkg/cloudprovider/aws/amifamily"
	"github.com/aws/karpenter/pkg/cloudprovider/aws/apis/v1alpha1"
	"github.com/aws/karpenter/pkg/scheduling"
	"github.com/aws/karpenter/pkg/utils/functional"
	"github.com/aws/karpenter/pkg/utils/injection"
	"github.com/aws/karpenter/pkg/utils/project"
	"github.com/aws/karpenter/pkg/utils/sets"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/transport"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
)

const (
	// CacheTTL restricts QPS to AWS APIs to this interval for verifying setup
	// resources. This value represents the maximum eventual consistency between
	// AWS actual state and the controller's ability to provision those
	// resources. Cache hits enable faster provisioning and reduced API load on
	// AWS APIs, which can have a serious impact on performance and scalability.
	// DO NOT CHANGE THIS VALUE WITHOUT DUE CONSIDERATION
	CacheTTL = 60 * time.Second
	// CacheCleanupInterval triggers cache cleanup (lazy eviction) at this interval.
	CacheCleanupInterval = 10 * time.Minute
	// MaxInstanceTypes defines the number of instance type options to pass to CreateFleet
	MaxInstanceTypes = 20
)

func init() {
	v1alpha5.NormalizedLabels = functional.UnionStringMaps(v1alpha5.NormalizedLabels, map[string]string{"topology.ebs.csi.aws.com/zone": v1.LabelTopologyZone})
}

type CloudProvider struct {
	instanceTypeProvider *InstanceTypeProvider
	subnetProvider       *SubnetProvider
	instanceProvider     *InstanceProvider
}

func NewCloudProvider(ctx context.Context) *CloudProvider {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("aws"))
	sess := withUserAgent(session.Must(session.NewSession(
		request.WithRetryer(
			&aws.Config{STSRegionalEndpoint: endpoints.RegionalSTSEndpoint},
			client.DefaultRetryer{NumMaxRetries: client.DefaultRetryerMaxNumRetries},
		),
	)))
	if *sess.Config.Region == "" {
		logging.FromContext(ctx).Debug("AWS region not configured, asking EC2 Instance Metadata Service")
		*sess.Config.Region = getRegionFromIMDS(sess)
	}
	logging.FromContext(ctx).Debugf("Using AWS region %s", *sess.Config.Region)
	ec2api := ec2.New(sess)
	subnetProvider := NewSubnetProvider(ec2api)
	instanceTypeProvider := NewInstanceTypeProvider(ec2api, subnetProvider)
	return &CloudProvider{
		instanceTypeProvider: instanceTypeProvider,
		subnetProvider:       subnetProvider,
		instanceProvider: &InstanceProvider{ec2api, instanceTypeProvider, subnetProvider,
			NewLaunchTemplateProvider(
				ctx,
				ec2api,
				injection.KubernetesInterface(ctx),
				amifamily.New(ssm.New(sess), cache.New(CacheTTL, CacheCleanupInterval)),
				NewSecurityGroupProvider(ec2api),
				getCABundle(ctx),
			),
		},
	}
}

// Create a node given the constraints.
func (c *CloudProvider) Create(ctx context.Context, nodeRequest *cloudprovider.NodeRequest) (*v1.Node, error) {
	vendorConstraints, err := v1alpha1.Deserialize(nodeRequest.Template.Provider)
	if err != nil {
		return nil, err
	}
	return c.instanceProvider.Create(ctx, vendorConstraints, nodeRequest)
}

// GetInstanceTypes returns all available InstanceTypes
func (c *CloudProvider) GetInstanceTypes(ctx context.Context) ([]cloudprovider.InstanceType, error) {
	return c.instanceTypeProvider.Get(ctx)
}

func (c *CloudProvider) Delete(ctx context.Context, node *v1.Node) error {
	return c.instanceProvider.Terminate(ctx, node)
}

func (c *CloudProvider) GetRequirements(ctx context.Context, provider *v1alpha5.Provider) (scheduling.Requirements, error) {
	awsprovider, err := v1alpha1.Deserialize(provider)
	if err != nil {
		return scheduling.NewRequirements(), apis.ErrGeneric(err.Error())
	}
	// Constrain AZs from subnets
	subnets, err := c.subnetProvider.Get(ctx, awsprovider)
	if err != nil {
		return scheduling.NewRequirements(), err
	}
	zones := sets.NewSet()
	for _, subnet := range subnets {
		zones.Insert(aws.StringValue(subnet.AvailabilityZone))
	}
	requirements := scheduling.Requirements{v1.LabelTopologyZone: zones}
	return requirements, nil
}

// Validate the provisioner
func (c *CloudProvider) Validate(ctx context.Context, provisioner *v1alpha5.Provisioner) *apis.FieldError {
	provider, err := v1alpha1.Deserialize(provisioner.Spec.Provider)
	if err != nil {
		return apis.ErrGeneric(err.Error())
	}
	return provider.Validate()
}

// Default the provisioner
func (c *CloudProvider) Default(ctx context.Context, provisioner *v1alpha5.Provisioner) {
	defaultLabels(provisioner)
}

func defaultLabels(provisioner *v1alpha5.Provisioner) {
	for key, value := range map[string]string{
		v1alpha5.LabelCapacityType: ec2.DefaultTargetCapacityTypeOnDemand,
		v1.LabelArchStable:         v1alpha5.ArchitectureAmd64,
	} {
		hasLabel := false
		if _, ok := provisioner.Spec.Labels[key]; ok {
			hasLabel = true
		}
		for _, requirement := range provisioner.Spec.Requirements {
			if requirement.Key == key {
				hasLabel = true
			}
		}
		if !hasLabel {
			provisioner.Spec.Requirements = append(provisioner.Spec.Requirements, v1.NodeSelectorRequirement{
				Key: key, Operator: v1.NodeSelectorOpIn, Values: []string{value},
			})
		}
	}
}

// Name returns the CloudProvider implementation name.
func (c *CloudProvider) Name() string {
	return "aws"
}

// get the current region from EC2 IMDS
func getRegionFromIMDS(sess *session.Session) string {
	region, err := ec2metadata.New(sess).Region()
	if err != nil {
		panic(fmt.Sprintf("Failed to call the metadata server's region API, %s", err))
	}
	return region
}

// withUserAgent adds a karpenter specific user-agent string to AWS session
func withUserAgent(sess *session.Session) *session.Session {
	userAgent := fmt.Sprintf("karpenter.sh-%s", project.Version)
	sess.Handlers.Build.PushBack(request.MakeAddToUserAgentFreeFormHandler(userAgent))
	return sess
}

func getCABundle(ctx context.Context) *string {
	// Discover CA Bundle from the REST client. We could alternatively
	// have used the simpler client-go InClusterConfig() method.
	// However, that only works when Karpenter is running as a Pod
	// within the same cluster it's managing.
	restConfig := injection.RestConfig(ctx)
	if restConfig == nil {
		return nil
	}
	transportConfig, err := restConfig.TransportConfig()
	if err != nil {
		logging.FromContext(ctx).Fatalf("Unable to discover caBundle, loading transport config, %v", err)
		return nil
	}
	_, err = transport.TLSConfigFor(transportConfig) // fills in CAData!
	if err != nil {
		logging.FromContext(ctx).Fatalf("Unable to discover caBundle, loading TLS config, %v", err)
		return nil
	}
	logging.FromContext(ctx).Debugf("Discovered caBundle, length %d", len(transportConfig.TLS.CAData))
	return ptr.String(base64.StdEncoding.EncodeToString(transportConfig.TLS.CAData))
}
