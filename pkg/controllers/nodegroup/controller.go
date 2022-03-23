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

package nodegroup

import (
	"context"
	"fmt"
	"math/rand"

	"go.uber.org/multierr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/workqueue"
	"knative.dev/pkg/logging"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/imdario/mergo"

	"github.com/aws/karpenter/pkg/apis/provisioning/v1alpha5"
	"github.com/aws/karpenter/pkg/cloudprovider"
	"github.com/aws/karpenter/pkg/controllers/provisioning/binpacking"
	"github.com/aws/karpenter/pkg/utils/functional"
)

var (
	controllerName        = "nodegroup"
	NodeGroupLabelNameKey = "karpenter.sh/node-group"
)

// Controller for the resource
type Controller struct {
	kubeClient    client.Client
	coreV1Client  corev1.CoreV1Interface
	cloudProvider cloudprovider.CloudProvider
}

// NewController constructs a controller
func NewController(kubeClient client.Client, coreV1Client corev1.CoreV1Interface, cloudProvider cloudprovider.CloudProvider) *Controller {
	return &Controller{
		kubeClient:    kubeClient,
		coreV1Client:  coreV1Client,
		cloudProvider: cloudProvider,
	}
}

// Reconcile the resource
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named(controllerName).With("nodegroup", req.String()))
	nodeGroup := &v1alpha5.NodeGroup{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, nodeGroup); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	nodes, err := c.List(ctx, nodeGroup)
	if err != nil {
		return reconcile.Result{}, err
	}
	if err := c.Add(ctx, nodeGroup, nodes); err != nil {
		return reconcile.Result{}, fmt.Errorf("adding nodes, %w", err)
	}
	if err := c.Remove(ctx, nodeGroup, nodes); err != nil {
		return reconcile.Result{}, fmt.Errorf("removing nodes, %w", err)
	}
	return reconcile.Result{}, nil
}

// Add nodes until replicas match
func (c *Controller) Add(ctx context.Context, nodeGroup *v1alpha5.NodeGroup, nodes []*v1.Node) error {
	if len(nodes) >= int(nodeGroup.Spec.Replicas) {
		return nil
	}
	count := int(nodeGroup.Spec.Replicas) - len(nodes)
	logging.FromContext(ctx).Infof("Found %d/%d nodes, adding %d", len(nodes), nodeGroup.Spec.Replicas, count)
	// Compute viable instance types
	instanceTypes, err := c.cloudProvider.GetInstanceTypes(ctx, nodeGroup.Spec.Provider)
	if err != nil {
		return fmt.Errorf("listing instance types, %w", err)
	}

	nodeGroup.Spec.Labels = functional.UnionStringMaps(nodeGroup.Spec.Labels, map[string]string{NodeGroupLabelNameKey: nodeGroup.Name})
	nodeGroup.Spec.Requirements = v1alpha5.NewLabelRequirements(nodeGroup.Spec.Labels).
		Add(nodeGroup.Spec.Requirements.Requirements...).
		Add(cloudprovider.Requirements(instanceTypes).Requirements...)

	compatibleInstanceTypes := []cloudprovider.InstanceType{}
	for _, instanceType := range instanceTypes {
		if len(compatibleInstanceTypes) > binpacking.MaxInstanceTypes {
			break
		}
		if cloudprovider.Compatible(instanceType, nodeGroup.Spec.Requirements) {
			compatibleInstanceTypes = append(compatibleInstanceTypes, instanceType)
		}
	}
	// Create capacity
	var errs = make([]error, count)
	workqueue.ParallelizeUntil(ctx, count, count, func(i int) {
		errs[i] = c.cloudProvider.Create(ctx, &nodeGroup.Spec.Constraints, compatibleInstanceTypes, 1, func(node *v1.Node) error {
			if err := mergo.Merge(node, nodeGroup.Spec.Constraints.ToNode()); err != nil {
				return fmt.Errorf("merging cloud provider node, %w", err)
			}
			_, err := c.coreV1Client.Nodes().Create(ctx, node, metav1.CreateOptions{})
			return err
		})
		logging.FromContext(ctx).Infof("Created node %s", nodes[i].Name)
	})
	return multierr.Combine(errs...)
}

// Remove nodes until replicas match
func (c *Controller) Remove(ctx context.Context, nodeGroup *v1alpha5.NodeGroup, nodes []*v1.Node) error {
	for i := int(nodeGroup.Spec.Replicas); i < len(nodes); i++ {
		if err := c.kubeClient.Delete(ctx, nodes[i]); err != nil {
			return fmt.Errorf("deleting node, %w", err)
		}
		logging.FromContext(ctx).Info("Removing node %s", nodes[i].Name)
	}
	return nil
}

// List nodes matching this node group
func (c *Controller) List(ctx context.Context, nodeGroup *v1alpha5.NodeGroup) ([]*v1.Node, error) {
	nodes := &v1.NodeList{}
	if err := c.kubeClient.List(ctx, nodes, client.MatchingLabels(map[string]string{NodeGroupLabelNameKey: nodeGroup.Name})); err != nil {
		return nil, err
	}
	result := []*v1.Node{}
	for i := range nodes.Items {
		if nodes.Items[i].DeletionTimestamp == nil {
			result = append(result, &nodes.Items[i])
		}
	}
	rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	return result, nil
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.
		NewControllerManagedBy(m).
		Named(controllerName).
		For(&v1alpha5.NodeGroup{}).
		Owns(&v1.Node{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 10}).
		Complete(c)
}
