package submarinerbrokerinfo

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"

	apiconfigv1 "github.com/openshift/api/config/v1"
	configv1alpha1 "github.com/stolostron/submariner-addon/pkg/apis/submarinerconfig/v1alpha1"
	"github.com/stolostron/submariner-addon/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	catalogName                   = "submariner"
	defaultCatalogSource          = "redhat-operators"
	defaultCatalogSourceNamespace = "openshift-marketplace"
	defaultCableDriver            = "libreswan"
	defaultInstallationNamespace  = "open-cluster-management-agent-addon"
	brokerAPIServer               = "BROKER_API_SERVER"
	ocpInfrastructureName         = "cluster"
	ocpAPIServerName              = "cluster"
	ocpConfigNamespace            = "openshift-config"
	brokerSuffix                  = "broker"
	namespaceMaxLength            = 63
)

var (
	infrastructureGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "infrastructures",
	}

	apiServerGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "apiservers",
	}
)

type SubmarinerBrokerInfo struct {
	NATEnabled                bool
	LoadBalancerEnabled       bool
	IPSecIKEPort              int
	IPSecNATTPort             int
	InstallationNamespace     string
	BrokerAPIServer           string
	BrokerNamespace           string
	BrokerToken               string
	BrokerCA                  string
	IPSecPSK                  string
	CableDriver               string
	ClusterName               string
	ClusterCIDR               string
	GlobalCIDR                string
	ServiceCIDR               string
	CatalogChannel            string
	CatalogName               string
	CatalogSource             string
	CatalogSourceNamespace    string
	CatalogStartingCSV        string
	SubmarinerGatewayImage    string
	SubmarinerRouteAgentImage string
	SubmarinerGlobalnetImage  string
	LighthouseAgentImage      string
	LighthouseCoreDNSImage    string
}

// Get retrieves submariner broker information consolidated with hub information.
func Get(
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	clusterName string,
	brokeNamespace string,
	submarinerConfig *configv1alpha1.SubmarinerConfig,
	installationNamespace string) (*SubmarinerBrokerInfo, error) {
	brokerInfo := &SubmarinerBrokerInfo{
		CableDriver:            defaultCableDriver,
		IPSecNATTPort:          constants.SubmarinerNatTPort,
		BrokerNamespace:        brokeNamespace,
		ClusterName:            clusterName,
		CatalogName:            catalogName,
		CatalogSource:          defaultCatalogSource,
		CatalogSourceNamespace: defaultCatalogSourceNamespace,
		InstallationNamespace:  defaultInstallationNamespace,
	}

	if installationNamespace != "" {
		brokerInfo.InstallationNamespace = installationNamespace
	}

	apiServer, err := getBrokerAPIServer(dynamicClient)
	if err != nil {
		return nil, err
	}

	brokerInfo.BrokerAPIServer = apiServer

	ipSecPSK, err := getIPSecPSK(kubeClient, brokeNamespace)
	if err != nil {
		return nil, err
	}

	brokerInfo.IPSecPSK = ipSecPSK

	token, ca, err := getBrokerTokenAndCA(kubeClient, dynamicClient, brokeNamespace, clusterName, apiServer)
	if err != nil {
		return nil, err
	}

	brokerInfo.BrokerCA = ca
	brokerInfo.BrokerToken = token

	applySubmarinerConfig(brokerInfo, submarinerConfig)

	return brokerInfo, nil
}

func applySubmarinerConfig(brokerInfo *SubmarinerBrokerInfo, submarinerConfig *configv1alpha1.SubmarinerConfig) {
	if submarinerConfig == nil {
		return
	}

	brokerInfo.NATEnabled = submarinerConfig.Spec.NATTEnable
	brokerInfo.LoadBalancerEnabled = submarinerConfig.Spec.LoadBalancerEnable

	if submarinerConfig.Spec.GlobalCIDR != "" {
		brokerInfo.GlobalCIDR = submarinerConfig.Spec.GlobalCIDR
	}

	if submarinerConfig.Spec.CableDriver != "" {
		brokerInfo.CableDriver = submarinerConfig.Spec.CableDriver
	}

	if submarinerConfig.Spec.IPSecIKEPort != 0 {
		brokerInfo.IPSecIKEPort = submarinerConfig.Spec.IPSecIKEPort
	}

	if submarinerConfig.Spec.IPSecNATTPort != 0 {
		brokerInfo.IPSecNATTPort = submarinerConfig.Spec.IPSecNATTPort
	}

	if submarinerConfig.Spec.SubscriptionConfig.Channel != "" {
		brokerInfo.CatalogChannel = submarinerConfig.Spec.SubscriptionConfig.Channel
	}

	if submarinerConfig.Spec.SubscriptionConfig.Source != "" {
		brokerInfo.CatalogSource = submarinerConfig.Spec.SubscriptionConfig.Source
	}

	if submarinerConfig.Spec.SubscriptionConfig.SourceNamespace != "" {
		brokerInfo.CatalogSourceNamespace = submarinerConfig.Spec.SubscriptionConfig.SourceNamespace
	}

	if submarinerConfig.Spec.SubscriptionConfig.StartingCSV != "" {
		brokerInfo.CatalogStartingCSV = submarinerConfig.Spec.SubscriptionConfig.StartingCSV
	}

	applySubmarinerImageConfig(brokerInfo, submarinerConfig)
}

func applySubmarinerImageConfig(brokerInfo *SubmarinerBrokerInfo, submarinerConfig *configv1alpha1.SubmarinerConfig) {
	if submarinerConfig.Spec.ImagePullSpecs.SubmarinerImagePullSpec != "" {
		brokerInfo.SubmarinerGatewayImage = submarinerConfig.Spec.ImagePullSpecs.SubmarinerImagePullSpec
	}

	if submarinerConfig.Spec.ImagePullSpecs.SubmarinerRouteAgentImagePullSpec != "" {
		brokerInfo.SubmarinerRouteAgentImage = submarinerConfig.Spec.ImagePullSpecs.SubmarinerRouteAgentImagePullSpec
	}

	if submarinerConfig.Spec.ImagePullSpecs.LighthouseCoreDNSImagePullSpec != "" {
		brokerInfo.LighthouseCoreDNSImage = submarinerConfig.Spec.ImagePullSpecs.LighthouseCoreDNSImagePullSpec
	}

	if submarinerConfig.Spec.ImagePullSpecs.LighthouseAgentImagePullSpec != "" {
		brokerInfo.LighthouseAgentImage = submarinerConfig.Spec.ImagePullSpecs.LighthouseAgentImagePullSpec
	}

	if submarinerConfig.Spec.ImagePullSpecs.SubmarinerGlobalnetImagePullSpec != "" {
		brokerInfo.SubmarinerGlobalnetImage = submarinerConfig.Spec.ImagePullSpecs.SubmarinerGlobalnetImagePullSpec
	}
}

func getIPSecPSK(client kubernetes.Interface, brokerNamespace string) (string, error) {
	secret, err := client.CoreV1().Secrets(brokerNamespace).Get(context.TODO(), constants.IPSecPSKSecretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get broker IPSEC PSK secret %v/%v: %w", brokerNamespace, constants.IPSecPSKSecretName, err)
	}

	return base64.StdEncoding.EncodeToString(secret.Data["psk"]), nil
}

func getBrokerAPIServer(dynamicClient dynamic.Interface) (string, error) {
	infrastructureConfig, err := dynamicClient.Resource(infrastructureGVR).Get(context.TODO(), ocpInfrastructureName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			apiServer := os.Getenv(brokerAPIServer)
			if apiServer == "" {
				return "", fmt.Errorf("failed to get apiserver in env %v", brokerAPIServer)
			}

			return apiServer, nil
		}

		return "", fmt.Errorf("failed to get infrastructures cluster: %w", err)
	}

	apiServer, found, err := unstructured.NestedString(infrastructureConfig.Object, "status", "apiServerURL")
	if err != nil || !found {
		return "", fmt.Errorf("failed to get apiServerURL in infrastructures cluster: %v: %w", found, err)
	}

	return strings.Trim(apiServer, "/:hpst"), nil
}

func getKubeAPIServerCA(kubeAPIServer string, kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) ([]byte, error) {
	kubeAPIServerURL, err := url.Parse(fmt.Sprintf("https://%s", kubeAPIServer))
	if err != nil {
		return nil, err
	}

	unstructuredAPIServer, err := dynamicClient.Resource(apiServerGVR).Get(context.TODO(), ocpAPIServerName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	apiServer := &apiconfigv1.APIServer{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredAPIServer.UnstructuredContent(), &apiServer); err != nil {
		return nil, err
	}

	for _, namedCert := range apiServer.Spec.ServingCerts.NamedCertificates {
		for _, name := range namedCert.Names {
			if !strings.EqualFold(name, kubeAPIServerURL.Hostname()) {
				continue
			}

			secretName := namedCert.ServingCertificate.Name
			secret, err := kubeClient.CoreV1().Secrets(ocpConfigNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}

			if secret.Type != corev1.SecretTypeTLS {
				return nil, fmt.Errorf("secret %s/%s should have type=kubernetes.io/tls", ocpConfigNamespace, secretName)
			}

			ca, ok := secret.Data["tls.crt"]
			if !ok {
				return nil, fmt.Errorf("failed to find data[tls.crt] in secret %s/%s", ocpConfigNamespace, secretName)
			}

			return ca, nil
		}
	}

	return nil, nil
}

func getBrokerTokenAndCA(kubeClient kubernetes.Interface, dynamicClient dynamic.Interface, brokerNS, clusterName,
	kubeAPIServer string) (token, ca string, err error) {
	sa, err := kubeClient.CoreV1().ServiceAccounts(brokerNS).Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get agent ServiceAccount %v/%v: %w", brokerNS, clusterName, err)
	}

	if len(sa.Secrets) < 1 {
		return "", "", fmt.Errorf("ServiceAccount %v does not have any secret", sa.Name)
	}

	brokerTokenPrefix := fmt.Sprintf("%s-token-", clusterName)

	for _, secret := range sa.Secrets {
		if strings.HasPrefix(secret.Name, brokerTokenPrefix) {
			tokenSecret, err := kubeClient.CoreV1().Secrets(brokerNS).Get(context.TODO(), secret.Name, metav1.GetOptions{})
			if err != nil {
				return "", "", fmt.Errorf("failed to get secret %v of agent ServiceAccount %v/%v: %w", secret.Name, brokerNS, clusterName, err)
			}

			if tokenSecret.Type == corev1.SecretTypeServiceAccountToken {
				// try to get ca from apiserver secret firstly, if the ca cannot be found, get it from sa
				kubeAPIServerCA, err := getKubeAPIServerCA(kubeAPIServer, kubeClient, dynamicClient)
				if err != nil {
					return "", "", err
				}

				if kubeAPIServerCA == nil {
					return string(tokenSecret.Data["token"]), base64.StdEncoding.EncodeToString(tokenSecret.Data["ca.crt"]), nil
				}

				return string(tokenSecret.Data["token"]), base64.StdEncoding.EncodeToString(kubeAPIServerCA), nil
			}
		}
	}

	return "", "", fmt.Errorf("ServiceAccount %v/%v does not have a secret of type token", brokerNS, clusterName)
}

func GenerateBrokerName(clusterSetName string) string {
	name := fmt.Sprintf("%s-%s", clusterSetName, brokerSuffix)
	if len(name) > namespaceMaxLength {
		truncatedClusterSetName := clusterSetName[(len(brokerSuffix) - 1):]

		return fmt.Sprintf("%s-%s", truncatedClusterSetName, brokerSuffix)
	}

	return name
}
