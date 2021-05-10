package resourcerequestoperator

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	discoveryv1alpha1 "github.com/liqotech/liqo/apis/discovery/v1alpha1"
)

// ResourceRequestReconciler reconciles a ResourceRequest object.
type ResourceRequestReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	ClusterID string
}

// ResourceToOffer is a custom struct to encapsulate cluster's ResourceList.
type ResourceToOffer struct {
	Offers corev1.ResourceList
}

// ResourceToOffer is a placeholder var with fake cluster resources.
var resources ResourceToOffer

const (
	offerPrefix = "resourceoffer-"
	timeToLive  = 30 * time.Minute
)

// +kubebuilder:rbac:groups=discovery.liqo.io,resources=resourceRequests,verbs=get;list;watch;create;update;patch;
// +kubebuilder:rbac:groups=discovery.liqo.io,resources=resourceRequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sharing.liqo.io,resources=resourceOffers,verbs=get;list;watch;create;update;patch;
// +kubebuilder:rbac:groups=sharing.liqo.io,resources=resourceOffers/status,verbs=get;update;patch

// Reconcile is the main function of the controller which reconciles ResourceRequest resources.
func (r *ResourceRequestReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	var resourceRequest discoveryv1alpha1.ResourceRequest
	err := r.Get(ctx, req.NamespacedName, &resourceRequest)
	if err != nil {
		klog.Errorf("%s -> unable to get resourceRequest %s: %s", r.ClusterID, req.NamespacedName, err)
		return ctrl.Result{}, nil
	}

	offerErr := r.generateResourceOffer(&resourceRequest)
	if offerErr != nil {
		klog.Errorf("%s -> Error generating resourceOffer: %s", r.ClusterID, offerErr)
		return ctrl.Result{}, offerErr
	}

	return ctrl.Result{}, nil
}

// SetupWithManager is the setup function of the controller.
func (r *ResourceRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&discoveryv1alpha1.ResourceRequest{}).
		Complete(r)
}
