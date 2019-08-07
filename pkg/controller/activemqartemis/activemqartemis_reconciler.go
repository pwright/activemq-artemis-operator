package activemqartemis

import (
	brokerv2alpha1 "github.com/rh-messaging/activemq-artemis-operator/pkg/apis/broker/v2alpha1"
	"github.com/rh-messaging/activemq-artemis-operator/pkg/resources/volumes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"strconv"
	"strings"
)

const (
	statefulSetSizeUpdated          = 1 << 0
	statefulSetClusterConfigUpdated = 1 << 1
	statefulSetSSLConfigUpdated     = 1 << 2
	statefulSetImageUpdated         = 1 << 3
	statefulSetPersistentUpdated    = 1 << 4
	statefulSetAioUpdated           = 1 << 5
	statefulSetCommonConfigUpdated  = 1 << 6
	statefulSetRequireLoginUpdated  = 1 << 7
)

type ActiveMQArtemisReconciler struct {
	statefulSetUpdates uint32
}

type ActiveMQArtemisIReconciler interface {
	Process(customResource *brokerv2alpha1.ActiveMQArtemis, currentStatefulSet *appsv1.StatefulSet) uint32
	ProcessDeploymentPlan(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) uint32
}

func (reconciler *ActiveMQArtemisReconciler) Process(customResource *brokerv2alpha1.ActiveMQArtemis, currentStatefulSet *appsv1.StatefulSet) uint32 {
	statefulSetUpdates := reconciler.ProcessDeploymentPlan(&customResource.Spec.DeploymentPlan, currentStatefulSet)

	return statefulSetUpdates
}

func (reconciler *ActiveMQArtemisReconciler) ProcessDeploymentPlan(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) uint32 {

	// Ensure the StatefulSet size is the same as the spec
	if *currentStatefulSet.Spec.Replicas != deploymentPlan.Size {
		currentStatefulSet.Spec.Replicas = &deploymentPlan.Size
		reconciler.statefulSetUpdates |= statefulSetSizeUpdated
	}

	if clusterConfigSyncCausedUpdateOn(deploymentPlan, currentStatefulSet) {
		reconciler.statefulSetUpdates |= statefulSetClusterConfigUpdated
	}

	if sslConfigSyncCausedUpdateOn(deploymentPlan, currentStatefulSet) {
		reconciler.statefulSetUpdates |= statefulSetSSLConfigUpdated
	}

	if imageSyncCausedUpdateOn(deploymentPlan, currentStatefulSet) {
		reconciler.statefulSetUpdates |= statefulSetImageUpdated
	}

	if aioSyncCausedUpdateOn(deploymentPlan, currentStatefulSet) {
		reconciler.statefulSetUpdates |= statefulSetAioUpdated
	}

	if persistentSyncCausedUpdateOn(deploymentPlan, currentStatefulSet) {
		reconciler.statefulSetUpdates |= statefulSetPersistentUpdated
	}

	if commonConfigSyncCausedUpdateOn(deploymentPlan, currentStatefulSet) {
		reconciler.statefulSetUpdates |= statefulSetCommonConfigUpdated
	}

	if requireLoginSyncCausedUpdateOn(deploymentPlan, currentStatefulSet) {
		reconciler.statefulSetUpdates |= statefulSetRequireLoginUpdated
	}

	return reconciler.statefulSetUpdates
}

// https://stackoverflow.com/questions/37334119/how-to-delete-an-element-from-a-slice-in-golang
func remove(s []corev1.EnvVar, i int) []corev1.EnvVar {
	s[i] = s[len(s)-1]
	// We do not need to put s[i] at the end, as it will be discarded anyway
	return s[:len(s)-1]
}

func clusterConfigSyncCausedUpdateOn(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundClustered := false
	foundClusterUser := false
	foundClusterPassword := false

	clusteredNeedsUpdate := false
	clusterUserNeedsUpdate := false
	clusterPasswordNeedsUpdate := false

	statefulSetUpdated := false

	clusterUserEnvVarSource := &corev1.EnvVarSource {
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "amq-credentials-secret",
			},
			Key:                  "clusterUser",
			Optional:             nil,
		},
	}

	clusterPasswordEnvVarSource := &corev1.EnvVarSource {
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "amq-credentials-secret",
			},
			Key:                  "clusterPassword",
			Optional:             nil,
		},
	}

	// TODO: Remove yuck
	// ensure password and username are valid if can't via openapi validation?
	{
		envVarArray := []corev1.EnvVar{}
		// Find the existing values
		for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
			if v.Name == "AMQ_CLUSTERED" {
				foundClustered = true
				//if v.Value == "false" {
				boolValue, _ := strconv.ParseBool(v.Value)
				if boolValue != deploymentPlan.Clustered {
					clusteredNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_CLUSTER_USER" {
				foundClusterUser = true
				if v.Value != deploymentPlan.ClusterUser {
					clusterUserNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_CLUSTER_PASSWORD" {
				foundClusterPassword = true
				if v.Value != deploymentPlan.ClusterPassword {
					clusterPasswordNeedsUpdate = true
				}
			}
		}

		if !foundClustered || clusteredNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_CLUSTERED",
				strconv.FormatBool(deploymentPlan.Clustered),
				nil,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if !foundClusterUser || clusterUserNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_CLUSTER_USER",
				"",
				clusterUserEnvVarSource,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if !foundClusterPassword || clusterPasswordNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_CLUSTER_PASSWORD",
				"",
				clusterPasswordEnvVarSource, //nil,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if statefulSetUpdated {
			envVarArrayLen := len(envVarArray)
			if envVarArrayLen > 0 {
				for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
					for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
						if ("AMQ_CLUSTERED" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && clusteredNeedsUpdate) ||
							("AMQ_CLUSTER_USER" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && clusterUserNeedsUpdate) ||
							("AMQ_CLUSTER_PASSWORD" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && clusterPasswordNeedsUpdate) {
							currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
						}
					}
				}

				containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
				for i := 0; i < containerArrayLen; i++ {
					for j := 0; j < envVarArrayLen; j++ {
						currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, envVarArray[j])
					}
				}
			}
		}
	}

	return statefulSetUpdated
}

func sslConfigSyncCausedUpdateOn(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundKeystore := false
	foundKeystorePassword := false
	foundKeystoreTruststoreDir := false
	foundTruststore := false
	foundTruststorePassword := false

	keystoreNeedsUpdate := false
	keystorePasswordNeedsUpdate := false
	keystoreTruststoreDirNeedsUpdate := false
	truststoreNeedsUpdate := false
	truststorePasswordNeedsUpdate := false

	statefulSetUpdated := false

	// TODO: Remove yuck
	// ensure password and username are valid if can't via openapi validation?
	//if customResource.Spec.SSLConfig.KeyStorePassword != "" &&
	//	customResource.Spec.SSLConfig.KeystoreFilename != "" {
	if false { // "TODO-FIX-REPLACE"

		envVarArray := []corev1.EnvVar{}
		// Find the existing values
		for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
			if v.Name == "AMQ_KEYSTORE" {
				foundKeystore = true
				//if v.Value != customResource.Spec.SSLConfig.KeystoreFilename {
				if false { // "TODO-FIX-REPLACE"
					keystoreNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_KEYSTORE_PASSWORD" {
				foundKeystorePassword = true
				//if v.Value != customResource.Spec.SSLConfig.KeyStorePassword {
				if false { // "TODO-FIX-REPLACE"
					keystorePasswordNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_KEYSTORE_TRUSTSTORE_DIR" {
				foundKeystoreTruststoreDir = true
				if v.Value != "/etc/amq-secret-volume" {
					keystoreTruststoreDirNeedsUpdate = true
				}
			}
		}

		if !foundKeystore || keystoreNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_KEYSTORE",
				"TODO-FIX-REPLACE",//customResource.Spec.SSLConfig.KeystoreFilename,
				nil,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if !foundKeystorePassword || keystorePasswordNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_KEYSTORE_PASSWORD",
				"TODO-FIX-REPLACE", //customResource.Spec.SSLConfig.KeyStorePassword,
				nil,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if !foundKeystoreTruststoreDir || keystoreTruststoreDirNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_KEYSTORE_TRUSTSTORE_DIR",
				"/etc/amq-secret-volume",
				nil,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if statefulSetUpdated {
			envVarArrayLen := len(envVarArray)
			if envVarArrayLen > 0 {
				for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
					for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
						if ("AMQ_KEYSTORE" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && keystoreNeedsUpdate) ||
							("AMQ_KEYSTORE_PASSWORD" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && keystorePasswordNeedsUpdate) ||
							("AMQ_KEYSTORE_TRUSTSTORE_DIR" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && keystoreTruststoreDirNeedsUpdate) {
							currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
						}
					}
				}

				containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
				for i := 0; i < containerArrayLen; i++ {
					for j := 0; j < envVarArrayLen; j++ {
						currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, envVarArray[j])
					}
				}
			}
		}
	} else {
		for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
			for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
				if "AMQ_KEYSTORE" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name ||
					"AMQ_KEYSTORE_PASSWORD" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name ||
					"AMQ_KEYSTORE_TRUSTSTORE_DIR" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name {
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
					statefulSetUpdated = true
				}
			}
		}
	}

	//if customResource.Spec.SSLConfig.TrustStorePassword != "" &&
	//	customResource.Spec.SSLConfig.TrustStoreFilename != "" {
	if false { // "TODO-FIX-REPLACE"
		envVarArray := []corev1.EnvVar{}
		// Find the existing values
		for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
			if v.Name == "AMQ_TRUSTSTORE" {
				foundTruststore = true
				//if v.Value != customResource.Spec.SSLConfig.TrustStoreFilename {
				if false { // "TODO-FIX-REPLACE"
					truststoreNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_TRUSTSTORE_PASSWORD" {
				foundTruststorePassword = true
				//if v.Value != customResource.Spec.SSLConfig.TrustStorePassword {
				if false { // "TODO-FIX-REPLACE"
					truststorePasswordNeedsUpdate = true
				}
			}
		}

		if !foundTruststore || truststoreNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_TRUSTSTORE",
				"TODO-FIX-REPLACE", //customResource.Spec.SSLConfig.TrustStoreFilename,
				nil,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if !foundTruststorePassword || truststorePasswordNeedsUpdate {
			newClusteredValue := corev1.EnvVar{
				"AMQ_TRUSTSTORE_PASSWORD",
				"TODO-FIX-REPLACE", //customResource.Spec.SSLConfig.TrustStorePassword,
				nil,
			}
			envVarArray = append(envVarArray, newClusteredValue)
			statefulSetUpdated = true
		}

		if statefulSetUpdated {
			envVarArrayLen := len(envVarArray)
			if envVarArrayLen > 0 {
				for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
					for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
						if ("AMQ_TRUSTSTORE" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && truststoreNeedsUpdate) ||
							("AMQ_TRUSTSTORE_PASSWORD" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && truststorePasswordNeedsUpdate) {
							currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
						}
					}
				}

				containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
				for i := 0; i < containerArrayLen; i++ {
					for j := 0; j < envVarArrayLen; j++ {
						currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, envVarArray[j])
					}
				}
			}
		}
	} else {
		for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
			for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
				if "AMQ_TRUSTSTORE" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name ||
					"AMQ_TRUSTSTORE_PASSWORD" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name {
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
					statefulSetUpdated = true
				}
			}
		}
	}

	if statefulSetUpdated {
		sslConfigSyncEnsureSecretVolumeMountExists(deploymentPlan, currentStatefulSet)
	}

	return statefulSetUpdated
}

func sslConfigSyncEnsureSecretVolumeMountExists(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) {

	secretVolumeExists := false
	secretVolumeMountExists := false

	for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Volumes); i++ {
		//if currentStatefulSet.Spec.Template.Spec.Volumes[i].Name == customResource.Spec.SSLConfig.SecretName {
		if false { // "TODO-FIX-REPLACE"
			secretVolumeExists = true
			break
		}
	}
	if !secretVolumeExists {
		volume := corev1.Volume{
			Name: "broker-secret-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "TODO-FIX-REPLACE", //customResource.Spec.SSLConfig.SecretName,
				},
			},
		}

		currentStatefulSet.Spec.Template.Spec.Volumes = append(currentStatefulSet.Spec.Template.Spec.Volumes, volume)
	}

	for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
		for j := 0; j < len(currentStatefulSet.Spec.Template.Spec.Containers[i].VolumeMounts); j++ {
			if currentStatefulSet.Spec.Template.Spec.Containers[i].VolumeMounts[j].Name == "broker-secret-volume" {
				secretVolumeMountExists = true
				break
			}
		}
		if !secretVolumeMountExists {
			volumeMount := corev1.VolumeMount{
				Name:      "broker-secret-volume",
				MountPath: "/etc/amq-secret-volume",
				ReadOnly:  true,
			}
			currentStatefulSet.Spec.Template.Spec.Containers[i].VolumeMounts = append(currentStatefulSet.Spec.Template.Spec.Containers[i].VolumeMounts, volumeMount)
		}
	}
}

func aioSyncCausedUpdateOn(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundAio := false
	foundNio := false
	var extraArgs string = ""
	extraArgsNeedsUpdate := false

	// Find the existing values
	for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
		if v.Name == "AMQ_EXTRA_ARGS" {
			if strings.Index(v.Value, "--aio") > -1 {
				foundAio = true
			}
			if strings.Index(v.Value, "--nio") > -1 {
				foundNio = true
			}
			extraArgs = v.Value
			break
		}
	}

	if "aio" == strings.ToLower(deploymentPlan.JournalType) && foundNio {
		extraArgs = strings.Replace(extraArgs, "--nio", "--aio", 1)
		extraArgsNeedsUpdate = true
	}

	if !("aio" == strings.ToLower(deploymentPlan.JournalType)) && foundAio {
		extraArgs = strings.Replace(extraArgs, "--aio", "--nio", 1)
		extraArgsNeedsUpdate = true
	}

	newExtraArgsValue := corev1.EnvVar{}
	if extraArgsNeedsUpdate {
		newExtraArgsValue = corev1.EnvVar{
			"AMQ_EXTRA_ARGS",
			extraArgs,
			nil,
		}

		containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
		for i := 0; i < containerArrayLen; i++ {
			//for j := 0; j < envVarArrayLen; j++ {
			for j := 0; j < len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env); j++ {
				if "AMQ_EXTRA_ARGS" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name {
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, newExtraArgsValue)
					break
				}
			}
		}
	}

	return extraArgsNeedsUpdate
}

func persistentSyncCausedUpdateOn(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundDataDir := false
	foundDataDirLogging := false

	dataDirNeedsUpdate := false
	dataDirLoggingNeedsUpdate := false

	statefulSetUpdated := false

	// TODO: Remove yuck
	// ensure password and username are valid if can't via openapi validation?
	if deploymentPlan.PersistenceEnabled {

		envVarArray := []corev1.EnvVar{}
		// Find the existing values
		for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
			if v.Name == "AMQ_DATA_DIR" {
				foundDataDir = true
				if v.Value == "false" {
					dataDirNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_DATA_DIR_LOGGING" {
				foundDataDirLogging = true
				if v.Value != "true" {
					dataDirLoggingNeedsUpdate = true
				}
			}
		}

		if !foundDataDir || dataDirNeedsUpdate {
			newDataDirValue := corev1.EnvVar{
				"AMQ_DATA_DIR",
				volumes.GLOBAL_DATA_PATH,
				nil,
			}
			envVarArray = append(envVarArray, newDataDirValue)
			statefulSetUpdated = true
		}

		if !foundDataDirLogging || dataDirLoggingNeedsUpdate {
			newDataDirLoggingValue := corev1.EnvVar{
				"AMQ_DATA_DIR_LOGGING",
				"true",
				nil,
			}
			envVarArray = append(envVarArray, newDataDirLoggingValue)
			statefulSetUpdated = true
		}

		if statefulSetUpdated {
			envVarArrayLen := len(envVarArray)
			if envVarArrayLen > 0 {
				for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
					for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
						if ("AMQ_DATA_DIR" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && dataDirNeedsUpdate) ||
							("AMQ_DATA_DIR_LOGGING" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && dataDirLoggingNeedsUpdate) {
							currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
						}
					}
				}

				containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
				for i := 0; i < containerArrayLen; i++ {
					for j := 0; j < envVarArrayLen; j++ {
						currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, envVarArray[j])
					}
				}
			}
		}
	} else {

		for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
			for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
				if "AMQ_DATA_DIR" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name ||
					"AMQ_DATA_DIR_LOGGING" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name {
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
					statefulSetUpdated = true
				}
			}
		}
	}

	return statefulSetUpdated
}

func imageSyncCausedUpdateOn(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	// At implementation time only one container
	if strings.Compare(currentStatefulSet.Spec.Template.Spec.Containers[0].Image, deploymentPlan.Image) != 0 {
		containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
		for i := 0; i < containerArrayLen; i++ {
			currentStatefulSet.Spec.Template.Spec.Containers[i].Image = deploymentPlan.Image
		}
		return true
	}

	return false
}

func commonConfigSyncCausedUpdateOn(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundCommonUser := false
	foundCommonPassword := false

	commonUserNeedsUpdate := false
	commonPasswordNeedsUpdate := false

	statefulSetUpdated := false

	// TODO: Remove yuck
	// ensure password and username are valid if can't via openapi validation?
	if deploymentPlan.Password != "" &&
		deploymentPlan.User != "" {

		envVarArray := []corev1.EnvVar{}
		// Find the existing values
		for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
			if v.Name == "AMQ_USER" {
				foundCommonUser = true
				if v.Value != deploymentPlan.User {
					commonUserNeedsUpdate = true
				}
			}
			if v.Name == "AMQ_PASSWORD" {
				foundCommonPassword = true
				if v.Value != deploymentPlan.Password {
					commonPasswordNeedsUpdate = true
				}
			}
		}

		if !foundCommonUser || commonUserNeedsUpdate {
			newCommonedValue := corev1.EnvVar{
				"AMQ_USER",
				deploymentPlan.User,
				nil,
			}
			envVarArray = append(envVarArray, newCommonedValue)
			statefulSetUpdated = true
		}

		if !foundCommonPassword || commonPasswordNeedsUpdate {
			newCommonedValue := corev1.EnvVar{
				"AMQ_PASSWORD",
				deploymentPlan.Password,
				nil,
			}
			envVarArray = append(envVarArray, newCommonedValue)
			statefulSetUpdated = true
		}

		if statefulSetUpdated {
			envVarArrayLen := len(envVarArray)
			if envVarArrayLen > 0 {
				for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
					for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
						if ("AMQ_USER" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && commonUserNeedsUpdate) ||
							("AMQ_PASSWORD" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && commonPasswordNeedsUpdate) {
							currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
						}
					}
				}

				containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
				for i := 0; i < containerArrayLen; i++ {
					for j := 0; j < envVarArrayLen; j++ {
						currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, envVarArray[j])
					}
				}
			}
		}
	}

	return statefulSetUpdated
}

func requireLoginSyncCausedUpdateOn(deploymentPlan *brokerv2alpha1.DeploymentPlanType, currentStatefulSet *appsv1.StatefulSet) bool {

	foundRequireLogin := false
	requireLoginNeedsUpdate := false

	statefulSetUpdated := false

	// Find the existing values
	for _, v := range currentStatefulSet.Spec.Template.Spec.Containers[0].Env {
		if v.Name == "AMQ_REQUIRE_LOGIN" {
			foundRequireLogin = true
			boolValue, _ := strconv.ParseBool(v.Value)
			if boolValue != deploymentPlan.RequireLogin {
				requireLoginNeedsUpdate = true
			}
		}
	}

	envVarArray := []corev1.EnvVar{}
	if !foundRequireLogin || requireLoginNeedsUpdate {
		newRequireLoginValue := corev1.EnvVar{
			"AMQ_REQUIRE_LOGIN",
			strconv.FormatBool(deploymentPlan.RequireLogin),
			nil,
		}
		envVarArray = append(envVarArray, newRequireLoginValue)
		statefulSetUpdated = true
	}

	if statefulSetUpdated {
		envVarArrayLen := len(envVarArray)
		if envVarArrayLen > 0 {
			for i := 0; i < len(currentStatefulSet.Spec.Template.Spec.Containers); i++ {
				for j := len(currentStatefulSet.Spec.Template.Spec.Containers[i].Env) - 1; j >= 0; j-- {
					if ("AMQ_REQUIRE_LOGIN" == currentStatefulSet.Spec.Template.Spec.Containers[i].Env[j].Name && requireLoginNeedsUpdate) {
						currentStatefulSet.Spec.Template.Spec.Containers[i].Env = remove(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, j)
					}
				}
			}

			containerArrayLen := len(currentStatefulSet.Spec.Template.Spec.Containers)
			for i := 0; i < containerArrayLen; i++ {
				for j := 0; j < envVarArrayLen; j++ {
					currentStatefulSet.Spec.Template.Spec.Containers[i].Env = append(currentStatefulSet.Spec.Template.Spec.Containers[i].Env, envVarArray[j])
				}
			}
		}
	}

	return statefulSetUpdated
}
