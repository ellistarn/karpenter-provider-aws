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

package injection

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/karpenter/pkg/cloudprovider"
	"github.com/aws/karpenter/pkg/controllers/state"
	"github.com/aws/karpenter/pkg/events"
	"github.com/aws/karpenter/pkg/utils/global"
)

func get(ctx context.Context, key interface{}) interface{} {
	value := ctx.Value(key)
	if value == nil {
		panic(fmt.Sprintf("nil value for key %s and context %s", reflect.TypeOf(key), ctx))
	}
	return value
}

type cloudProviderKey struct{}

func WithCloudProvider(ctx context.Context, value cloudprovider.CloudProvider) context.Context {
	return context.WithValue(ctx, cloudProviderKey{}, value)
}

func CloudProvider(ctx context.Context) cloudprovider.CloudProvider {
	return get(ctx, cloudProviderKey{}).(cloudprovider.CloudProvider)
}

type kubeClientKey struct{}

func WithKubeClient(ctx context.Context, value client.Client) context.Context {
	return context.WithValue(ctx, kubeClientKey{}, value)
}

func KubeClient(ctx context.Context) client.Client {
	return get(ctx, kubeClientKey{}).(client.Client)
}

type kubernetesInterfaceKey struct{}

func WithKubernetesInterface(ctx context.Context, value kubernetes.Interface) context.Context {
	return context.WithValue(ctx, kubernetesInterfaceKey{}, value)
}

func KubernetesInterface(ctx context.Context) kubernetes.Interface {
	return get(ctx, kubernetesInterfaceKey{}).(kubernetes.Interface)
}

type eventRecorderKey struct{}

func WithEventRecorder(ctx context.Context, value events.Recorder) context.Context {
	return context.WithValue(ctx, eventRecorderKey{}, value)
}

func EventRecorder(ctx context.Context) events.Recorder {
	return get(ctx, eventRecorderKey{}).(events.Recorder)
}

type clusterStateKey struct{}

func WithClusterState(ctx context.Context, value *state.Cluster) context.Context {
	return context.WithValue(ctx, clusterStateKey{}, value)
}

func ClusterState(ctx context.Context) *state.Cluster {
	return get(ctx, clusterStateKey{}).(*state.Cluster)
}

type namespacedNameKey struct{}

func WithNamespacedName(ctx context.Context, value types.NamespacedName) context.Context {
	return context.WithValue(ctx, namespacedNameKey{}, value)
}

func NamespacedName(ctx context.Context) types.NamespacedName {
	return get(ctx, namespacedNameKey{}).(types.NamespacedName)
}

type globalsKey struct{}

func WithGlobals(ctx context.Context, value global.Options) context.Context {
	return context.WithValue(ctx, globalsKey{}, value)
}

func Globals(ctx context.Context) global.Options {
	return get(ctx, globalsKey{}).(global.Options)
}

type restConfigKey struct{}

func WithRestConfig(ctx context.Context, value *rest.Config) context.Context {
	return context.WithValue(ctx, restConfigKey{}, value)
}

func RestConfig(ctx context.Context) *rest.Config {
	return get(ctx, restConfigKey{}).(*rest.Config)
}

type controllerNameKey struct{}

func WithControllerName(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, controllerNameKey{}, value)
}

func ControllerName(ctx context.Context) string {
	return get(ctx, controllerNameKey{}).(string)
}
