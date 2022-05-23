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

package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"knative.dev/pkg/apis"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/aws/karpenter/pkg/apis/provisioning/v1alpha5"
	"github.com/aws/karpenter/pkg/cloudprovider"
	"github.com/aws/karpenter/pkg/metrics"
	"github.com/aws/karpenter/pkg/scheduling"
	"github.com/aws/karpenter/pkg/utils/injection"
)

const (
	metricLabelController = "controller"
	metricLabelMethod     = "method"
	metricLabelProvider   = "provider"
)

var methodDurationHistogramVec = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: metrics.Namespace,
		Subsystem: "cloudprovider",
		Name:      "duration_seconds",
		Help:      "Duration of cloud provider method calls.",
	},
	[]string{
		metricLabelController,
		metricLabelMethod,
		metricLabelProvider,
	},
)

func init() {
	crmetrics.Registry.MustRegister(methodDurationHistogramVec)
}

type decorator struct {
	cloudprovider.CloudProvider
}

// Decorate returns a new `CloudProvider` instance that will delegate all method
// calls to the argument, `cloudProvider`, and publish aggregated latency metrics. The
// value used for the metric label, "controller", is taken from the `Context` object
// passed to the methods of `CloudProvider`.
//
// Do not decorate a `CloudProvider` multiple times or published metrics will contain
// duplicated method call counts and latencies.
func Decorate(cloudProvider cloudprovider.CloudProvider) cloudprovider.CloudProvider {
	return &decorator{cloudProvider}
}

func (d *decorator) Create(ctx context.Context, nodeRequest *cloudprovider.NodeRequest) (*v1.Node, error) {
	defer metrics.Measure(methodDurationHistogramVec.WithLabelValues(injection.ControllerName(ctx), "Create", d.Name()))()
	return d.CloudProvider.Create(ctx, nodeRequest)
}

func (d *decorator) Delete(ctx context.Context, node *v1.Node) error {
	defer metrics.Measure(methodDurationHistogramVec.WithLabelValues(injection.ControllerName(ctx), "Delete", d.Name()))()
	return d.CloudProvider.Delete(ctx, node)
}

func (d *decorator) GetInstanceTypes(ctx context.Context) ([]cloudprovider.InstanceType, error) {
	defer metrics.Measure(methodDurationHistogramVec.WithLabelValues(injection.ControllerName(ctx), "GetInstanceTypes", d.Name()))()
	return d.CloudProvider.GetInstanceTypes(ctx)
}

func (d *decorator) GetRequirements(ctx context.Context, provider *v1alpha5.Provider) (scheduling.Requirements, error) {
	defer metrics.Measure(methodDurationHistogramVec.WithLabelValues(injection.ControllerName(ctx), "GetInstanceTypes", d.Name()))()
	return d.CloudProvider.GetRequirements(ctx, provider)
}

func (d *decorator) Default(ctx context.Context, provisioner *v1alpha5.Provisioner) {
	defer metrics.Measure(methodDurationHistogramVec.WithLabelValues(injection.ControllerName(ctx), "Default", d.Name()))()
	d.CloudProvider.Default(ctx, provisioner)
}

func (d *decorator) Validate(ctx context.Context, provisioner *v1alpha5.Provisioner) *apis.FieldError {
	defer metrics.Measure(methodDurationHistogramVec.WithLabelValues(injection.ControllerName(ctx), "Validate", d.Name()))()
	return d.CloudProvider.Validate(ctx, provisioner)
}
