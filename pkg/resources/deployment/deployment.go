package deployment

import (
	"context"
	"fmt"
	csiv1 "github.com/dell/dell-csi-operator/api/v1"
	"github.com/dell/dell-csi-operator/pkg/constants"
	"github.com/dell/dell-csi-operator/pkg/ctrlconfig"
	"github.com/dell/dell-csi-operator/pkg/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"reflect"
	"time"

	//v1 "k8s.io/kubernetes/staging/src/k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SleepTime - minimum time to sleep before checking the state of controller pod
var SleepTime = 10 * time.Second

// New - Creates a deployment element for the given driver and component
func New(instance csiv1.CSIDriver, driverEnv []corev1.EnvVar, driverVolumeMounts []corev1.VolumeMount, podVolumes []corev1.Volume,
	args []string, sidecarMap map[csiv1.ImageType]ctrlconfig.SidecarParams, podConstraints csiv1.PodSchedulingConstraints) *appsv1.Deployment {
	var driver = instance.GetDriver()
	driverNamespace := instance.GetNamespace()
	replicas := driver.Replicas
	controllerName := instance.GetControllerName()
	labels := make(map[string]string)
	labels["app"] = controllerName
	containers := make([]corev1.Container, 0)

	var commonArgs []string
	if instance.GetDriverType() == csiv1.Isilon {
		for _, sideCarContainer := range driver.SideCars {
			if sideCarContainer.Name == "common" {
				commonArgs = sideCarContainer.Args
				break
			}
		}
	}

	args = append(args, "--leader-election")
	args = append(args, commonArgs...)

	containers = append(containers, resources.CreateContainerElement(
		csiv1.ImageTypeDriver, driver.Common.Image, driver.Common.ImagePullPolicy,
		args, driverEnv, driverVolumeMounts, nil, nil))

	for _, sideCarContainer := range driver.SideCars {
		// Add all sidecars except registrar and sdc-monitor for controller
		if sideCarContainer.Name != csiv1.ImageTypeRegistrar {
			if sideCarContainer.Name != csiv1.ImageTypeSdcmonitor {
				if sideCarContainer.Name != "common" {
					containerName := sideCarContainer.Name
					imageName := sideCarContainer.Image
					sidecarArgs := append(sidecarMap[containerName].Args, commonArgs...)
					containers = append(containers, resources.CreateContainerElement(
						containerName, imageName, sideCarContainer.ImagePullPolicy,
						sidecarArgs, sidecarMap[containerName].Envs,
						sidecarMap[containerName].VolumeMounts, nil, nil))
				}
			}
		}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			//Labels:          ,
			Name:            controllerName,
			Namespace:       driverNamespace,
			OwnerReferences: resources.GetOwnerReferences(instance),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:             &replicas,
			RevisionHistoryLimit: &constants.RevisionHistoryLimit,
			//PodManagementPolicy:  appsv1.OrderedReadyPodManagement,
			//ServiceName:          controllerName,

			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			//UpdateStrategy: appsv1.DeploymentUpdateStrategy{
			//	Type: appsv1.RollingUpdateDeploymentStrategyType,
			//},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: controllerName,
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "app",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{controllerName},
											},
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
					Containers:                    containers,
					DNSPolicy:                     corev1.DNSClusterFirst,
					RestartPolicy:                 corev1.RestartPolicyAlways,
					SchedulerName:                 corev1.DefaultSchedulerName,
					TerminationGracePeriodSeconds: &constants.TerminationGracePeriodSeconds,
					Volumes:                       podVolumes,
					Tolerations:                   podConstraints.Tolerations,
					NodeSelector:                  podConstraints.NodeSelector,
				},
			},
		},
	}
}

// SyncDeployment - Creates/Updates a Deployment
func SyncDeployment(ctx context.Context, deployment *appsv1.Deployment, client client.Client, reqLogger logr.Logger) error {
	found := &appsv1.Deployment{}
	err := client.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment", "Name", deployment.Name)
		err = client.Create(ctx, deployment)
		if err != nil {
			return err
		}
	} else if err != nil {
		reqLogger.Info("Unknown error.", "Error", err.Error())
		return err
	} else {
		reqLogger.Info("Updating Deployment", "Name:", deployment.Name)
		err = client.Update(ctx, deployment)
		if err != nil {
			return err
		}
	}

	return nil
}

// SyncControllerDeployment - Syncs a Deployment for controller
func SyncControllerDeployment(ctx context.Context, deployment *appsv1.Deployment, cclient client.Client, reqLogger logr.Logger) error {
	found := &appsv1.Deployment{}
	err := cclient.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new deployment", "Namespace", deployment.Namespace, "Name", deployment.Name)
		err = cclient.Create(ctx, deployment)
		if err != nil {
			return err
		}

		return nil
	} else if err != nil {
		reqLogger.Info("Unknown error.", "Error", err.Error())
		return err
	} else {
		reqLogger.Info("Updating Deployment", "Name:", deployment.Name)
		err = cclient.Update(ctx, deployment)
		if err != nil {
			return err
		}
		if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
			// Check if the pod spec is same as pod spec from stateful spec
			reqLogger.Info("Waiting 10 seconds before checking the status of controller pods")
			time.Sleep(SleepTime)
		}
		err := cclient.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Error(err, "Failed to find the deployment after upgrade. Internal error!")
			return err
		}

		podList := &corev1.PodList{}
		opts := []client.ListOption{
			client.InNamespace(deployment.GetNamespace()),
			client.MatchingLabels{"app": deployment.Name},
		}
		err = cclient.List(ctx, podList, opts...)

		podTemplateSpec := found.Spec.Template.Spec
		for _, controllerPod := range podList.Items {
			controllerPod := controllerPod
			podSpec := controllerPod.Spec
			if !comparePodSpec(podTemplateSpec, podSpec, reqLogger) {
				reqLogger.Info(fmt.Sprintf("Controller pod'spec doesn't match the spec from deployment. Pod Name: %s. Deleting it to force an update",
					controllerPod.Name))

				reqLogger.Info(fmt.Sprintf("Deleting the controller pod %s", controllerPod.Name))
				err = cclient.Delete(ctx, &controllerPod)
				if err != nil {
					reqLogger.Error(err, "Failed to delete the pod. Continuing")
				}
			}
		}
	}
	return nil
}

func comparePodSpec(spec1, spec2 corev1.PodSpec, reqLogger logr.Logger) bool {
	for _, container1 := range spec1.Containers {
		for _, container2 := range spec2.Containers {
			if container1.Name == container2.Name {
				if !reflect.DeepEqual(container1.Env, container2.Env) {
					reqLogger.Info("Environments don't match for", "container", container1.Name)
					return false
				}
				reqLogger.Info(fmt.Sprintf("Environment variables match for %s", container1.Name))
				if container1.Image != container2.Image {
					reqLogger.Info(fmt.Sprintf("Image (%s, %s) don't match for container %s",
						string(container1.Image), string(container2.Image), container1.Name))
					return false
				}
				if container1.ImagePullPolicy != container2.ImagePullPolicy {
					reqLogger.Info(fmt.Sprintf("ImagePullPolicy (%s, %s) don't match for container %s",
						string(container1.ImagePullPolicy), string(container2.ImagePullPolicy), container1.Name))
					return false
				}
			}
			break
		}
	}
	return true
}
