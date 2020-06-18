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

package advertisement_operator

import (
	"context"
	"github.com/go-logr/logr"
	protocolv1 "github.com/liqoTech/liqo/api/advertisement-operator/v1"
	policyv1 "github.com/liqoTech/liqo/api/cluster-config/v1"
	pkg "github.com/liqoTech/liqo/pkg/advertisement-operator"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	defaultClusterConfig = policyv1.ClusterConfigSpec{
		ResourceSharingPercentage:  0,
		MaxAcceptableAdvertisement: 0,
		AutoAccept:                 false,
	}
)

// AdvertisementReconciler reconciles a Advertisement object
type AdvertisementReconciler struct {
	client.Client
	Log              logr.Logger
	Scheme           *runtime.Scheme
	EventsRecorder   record.EventRecorder
	KubeletNamespace string
	KindEnvironment  bool
	VKImage          string
	InitVKImage      string
	HomeClusterId    string
	AcceptedAdvNum   int32
	clusterConfig    policyv1.ClusterConfigSpec
}

// +kubebuilder:rbac:groups=protocol.liqo.io,resources=advertisements,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=protocol.liqo.io,resources=advertisements/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events/status,verbs=get

func (r *AdvertisementReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("advertisement-controller", req.NamespacedName)
	r.Log = log

	// get advertisement
	var adv protocolv1.Advertisement
	if err := r.Get(ctx, req.NamespacedName, &adv); err != nil {
		if errors.IsNotFound(err) {
			// reconcile was triggered by a delete request
			log.Info("Advertisement " + req.Name + " deleted")
			// TODO: decrease r.AcceptedAdvNum if the advertisement was ACCEPTED
			return ctrl.Result{}, client.IgnoreNotFound(err)
		} else {
			// not managed error
			log.Error(err, "")
			return ctrl.Result{}, err
		}
	}

	// filter advertisements and create a virtual-kubelet only for the good ones
	if adv.Status.AdvertisementStatus == "" {
		r.checkAdvertisement(ctx, log, &adv)
		return ctrl.Result{}, nil
	}

	if adv.Status.AdvertisementStatus != "ACCEPTED" {
		log.Info("Advertisement " + adv.Name + " refused")
		return ctrl.Result{}, nil
	}

	if !r.KindEnvironment && adv.Status.RemoteRemappedPodCIDR == "" {
		r.Log.Info("advertisement not complete, remoteRemappedPodCIRD not set yet")
		return ctrl.Result{}, nil
	}

	if !adv.Status.VkCreated {
		err := r.createVirtualKubelet(ctx, log, &adv)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *AdvertisementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&protocolv1.Advertisement{}).
		Complete(r)
}

// check if the advertisement is interesting and set its status accordingly
func (r *AdvertisementReconciler) checkAdvertisement(ctx context.Context, log logr.Logger, adv *protocolv1.Advertisement) {

	if r.clusterConfig.AutoAccept {
		if r.AcceptedAdvNum < r.clusterConfig.MaxAcceptableAdvertisement {
			// the adv accepted so far are less than the configured maximum
			adv.Status.AdvertisementStatus = "ACCEPTED"
			r.AcceptedAdvNum++
			metav1.SetMetaDataAnnotation(&adv.ObjectMeta, "advertisementStatus", "accepted")
			r.recordEvent(log, "Advertisement "+adv.Name+" accepted", "Normal", "AdvertisementAccepted", adv)
		} else {
			// the maximum has been reached: cannot accept
			adv.Status.AdvertisementStatus = "REFUSED"
			metav1.SetMetaDataAnnotation(&adv.ObjectMeta, "advertisementStatus", "refused")
			r.recordEvent(log, "Advertisement "+adv.Name+" refused", "Normal", "AdvertisementRefused", adv)
		}

		if err := r.Status().Update(ctx, adv); err != nil {
			log.Error(err, "unable to update Advertisement status")
		}
	}
	//TODO: else branch with manual accept
}

func (r *AdvertisementReconciler) createVirtualKubelet(ctx context.Context, log logr.Logger, adv *protocolv1.Advertisement) error {

	// Create the base resources
	vkSa := &v1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "vkubelet-" + adv.Spec.ClusterId,
			Namespace:       r.KubeletNamespace,
			OwnerReferences: pkg.GetOwnerReference(adv),
		},
	}
	err := pkg.CreateOrUpdate(r.Client, ctx, log, vkSa)
	if err != nil {
		return err
	}
	vkCrb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "vkubelet-" + adv.Spec.ClusterId,
			OwnerReferences: pkg.GetOwnerReference(adv),
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", APIGroup: "", Name: "vkubelet-" + adv.Spec.ClusterId, Namespace: r.KubeletNamespace},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
	}
	err = pkg.CreateOrUpdate(r.Client, ctx, log, vkCrb)
	if err != nil {
		return err
	}
	// Create the virtual Kubelet
	deploy := pkg.CreateVkDeployment(adv, vkSa.Name, r.KubeletNamespace, r.VKImage, r.InitVKImage, r.HomeClusterId)
	err = pkg.CreateOrUpdate(r.Client, ctx, log, deploy)
	if err != nil {
		return err
	}

	r.recordEvent(log, "launching virtual-kubelet for cluster "+adv.Spec.ClusterId, "Normal", "VkCreated", adv)
	adv.Status.VkCreated = true
	if err := r.Status().Update(ctx, adv); err != nil {
		log.Error(err, "unable to update Advertisement status")
	}
	return nil
}

func (r *AdvertisementReconciler) recordEvent(log logr.Logger,
	msg string, eventType string, eventReason string,
	adv *protocolv1.Advertisement) {

	log.Info(msg)
	r.EventsRecorder.Event(adv, eventType, eventReason, msg)

}

func (r *AdvertisementReconciler) WatchConfiguration(kubeconfigPath string) error {
	configClient, err := policyv1.CreateClusterConfigClient(kubeconfigPath)
	if err != nil {
		r.Log.Info(err.Error())
		return err
	}

	watcher, err := configClient.Resource("clusterconfigs").Watch(metav1.ListOptions{})
	if err != nil {
		return err
	}

	go func() {
		for event := range watcher.ResultChan() {
			configuration, ok := event.Object.(*policyv1.ClusterConfig)
			if !ok {
				continue
			}

			// if first time, copy
			if r.clusterConfig == defaultClusterConfig {
				r.clusterConfig = configuration.Spec
			}

			switch event.Type {
			case watch.Added, watch.Modified:
				if configuration.Spec.MaxAcceptableAdvertisement > r.clusterConfig.MaxAcceptableAdvertisement {
					// the maximum has increased: check if there are refused advertisements which now can be accepted
					r.clusterConfig = configuration.Spec
					var advList protocolv1.AdvertisementList
					err = r.Client.List(context.Background(), &advList)
					if err != nil {
						r.Log.Error(err, "Unable to apply configuration: error listing Advertisements")
						continue
					}
					for _, adv := range advList.Items {
						if adv.Status.AdvertisementStatus == "REFUSED" {
							r.checkAdvertisement(context.Background(), r.Log, &adv)
						}
					}
				} else {
					// the maximum has decreased: if the accepted advertisements are too many, delete some of them
					r.clusterConfig = configuration.Spec
					if r.clusterConfig.MaxAcceptableAdvertisement < r.AcceptedAdvNum {
						var advList protocolv1.AdvertisementList
						err = r.Client.List(context.Background(), &advList, &client.ListOptions{})
						if err != nil {
							r.Log.Error(err, "Unable to apply configuration: error listing Advertisements")
							continue
						}
						for i := 0; i < int(r.AcceptedAdvNum-r.clusterConfig.MaxAcceptableAdvertisement); i++ {
							adv := advList.Items[i]
							if adv.Status.AdvertisementStatus == "ACCEPTED" {
								err = r.Client.Delete(context.Background(), &adv)
								if err != nil {
									r.Log.Error(err, "Unable to apply configuration: error deleting Advertisement "+adv.Name)
								}
								r.AcceptedAdvNum--
							}
						}
					}
				}
			case watch.Deleted:
				// TODO: set default config?
			}
		}
	}()
	return nil
}
