/*
Copyright 2022.

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

package controllers

import (
	"context"

	databasev1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/myctl"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// MysqlReconciler reconciles a Mysql object
type MysqlReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Myctl  *myctl.Myctl
}

//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=database.erda.cloud,resources=mysqls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=database.erda.cloud,resources=mysqls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=database.erda.cloud,resources=mysqls/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Mysql object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *MysqlReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	mysql := &databasev1.Mysql{}
	zeroResult := ctrl.Result{}

	if err := r.Get(ctx, req.NamespacedName, mysql); err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Myctl.Purge(req.NamespacedName)
		} else {
			log.Error(err, "unable to fetch Mysql")
		}
		return zeroResult, err
	}

	mysql.Default()

	if err := r.Myctl.SyncSpec(mysql); err != nil {
		return zeroResult, err
	}
	if err := r.Update(ctx, mysql); err != nil {
		return zeroResult, err
	}

	if err := r.Myctl.SyncStatus(mysql); err != nil {
		return zeroResult, err
	}
	if err := r.Status().Update(ctx, mysql); err != nil {
		return zeroResult, err
	}

	headlessSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mysql.BuildName(databasev1.HeadlessSuffix),
			Namespace: mysql.Namespace,
		},
	}
	opResult, err := ctrl.CreateOrUpdate(ctx, r.Client, headlessSvc, func() error {
		MutateSvc(mysql, headlessSvc, "x")
		return ctrl.SetControllerReference(mysql, headlessSvc, r.Scheme)
	})
	if err != nil {
		log.Error(err, "CreateOrUpdate headless svc failed")
		return ctrl.Result{}, err
	}
	log.Info("CreateOrUpdate headless svc succeeded", "OperationResult", opResult)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mysql.Name,
			Namespace: mysql.Namespace,
		},
	}
	opResult, err = ctrl.CreateOrUpdate(ctx, r.Client, sts, func() error {
		MutateSts(mysql, sts)
		return ctrl.SetControllerReference(mysql, sts, r.Scheme)
	})
	if err != nil {
		log.Error(err, "CreateOrUpdate sts failed")
		return ctrl.Result{}, err
	}
	log.Info("CreateOrUpdate sts succeeded", "OperationResult", opResult)

	wSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mysql.BuildName("write"),
			Namespace: mysql.Namespace,
		},
	}
	opResult, err = ctrl.CreateOrUpdate(ctx, r.Client, wSvc, func() error {
		MutateSvc(mysql, wSvc, "write")
		return ctrl.SetControllerReference(mysql, wSvc, r.Scheme)
	})
	if err != nil {
		log.Error(err, "CreateOrUpdate write svc failed")
		return ctrl.Result{}, err
	}
	log.Info("CreateOrUpdate write svc succeeded", "OperationResult", opResult)

	rSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mysql.BuildName("read"),
			Namespace: mysql.Namespace,
		},
	}
	opResult, err = ctrl.CreateOrUpdate(ctx, r.Client, rSvc, func() error {
		MutateSvc(mysql, rSvc, "read")
		return ctrl.SetControllerReference(mysql, rSvc, r.Scheme)
	})
	if err != nil {
		log.Error(err, "CreateOrUpdate read svc failed")
		return ctrl.Result{}, err
	}
	log.Info("CreateOrUpdate read svc succeeded", "OperationResult", opResult)

	return zeroResult, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MysqlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&databasev1.Mysql{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Watches(
			&source.Channel{Source: r.Myctl.C},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}
