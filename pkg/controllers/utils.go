package controllers

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"

	"github.com/aws/aws-cloud-map-mcs-controller-for-k8s/pkg/api/v1alpha1"
	"github.com/aws/aws-cloud-map-mcs-controller-for-k8s/pkg/model"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// DerivedServiceAnnotation annotates a ServiceImport with derived Service name
	DerivedServiceAnnotation = "multicluster.k8s.aws/derived-service"

	// LabelServiceImportName indicates the name of the multi-cluster service that an EndpointSlice belongs to.
	LabelServiceImportName = "multicluster.kubernetes.io/service-name"
)

// ServicePortToPort converts a k8s service port to internal model port
func ServicePortToPort(svcPort v1.ServicePort) model.Port {
	return model.Port{
		Name:       svcPort.Name,
		Port:       svcPort.Port,
		TargetPort: svcPort.TargetPort.String(),
		Protocol:   string(svcPort.Protocol),
	}
}

// ServiceImportPortToPort converts a service import port to an internal model port
func ServiceImportPortToPort(svcPort v1alpha1.ServicePort) model.Port {
	return model.Port{
		Name:     svcPort.Name,
		Port:     svcPort.Port,
		Protocol: string(svcPort.Protocol),
	}
}

// EndpointPortToPort converts a k8s endpoint port to an internal model port
func EndpointPortToPort(port discovery.EndpointPort) model.Port {
	return model.Port{
		Name:     *port.Name,
		Port:     *port.Port,
		Protocol: string(*port.Protocol),
	}
}

// PortToServicePort converts an internal model port to a k8s service port
func PortToServicePort(port model.Port) v1.ServicePort {
	return v1.ServicePort{
		Name:       port.Name,
		Protocol:   v1.Protocol(port.Protocol),
		Port:       port.Port,
		TargetPort: intstr.Parse(port.TargetPort),
	}
}

// PortToServiceImportPort converts an internal model port to a service import port
func PortToServiceImportPort(port model.Port) v1alpha1.ServicePort {
	return v1alpha1.ServicePort{
		Name:     port.Name,
		Protocol: v1.Protocol(port.Protocol),
		Port:     port.Port,
	}
}

// PortToEndpointPort converts an internal model port to a k8s endpoint port
func PortToEndpointPort(port model.Port) discovery.EndpointPort {
	protocol := v1.Protocol(port.Protocol)
	return discovery.EndpointPort{
		Name:     &port.Name,
		Protocol: &protocol,
		Port:     &port.Port,
	}
}

// ExtractServicePorts extracts all unique service ports from a slice of endpoints
func ExtractServicePorts(endpoints []*model.Endpoint) (servicePorts []*model.Port) {
	uniquePorts := make(map[string]model.Port)
	for _, ep := range endpoints {
		uniquePorts[ep.ServicePort.GetID()] = ep.ServicePort
	}
	for _, servicePort := range uniquePorts {
		portRef := servicePort
		servicePorts = append(servicePorts, &portRef)
	}
	return servicePorts
}

// ExtractEndpointPorts extracts all unique endpoint ports from a slice of endpoints
func ExtractEndpointPorts(endpoints []*model.Endpoint) (endpointPorts []*model.Port) {
	uniquePorts := make(map[string]model.Port)
	for _, ep := range endpoints {
		uniquePorts[ep.EndpointPort.GetID()] = ep.EndpointPort
	}
	for _, endpointPort := range uniquePorts {
		portRef := endpointPort
		endpointPorts = append(endpointPorts, &portRef)
	}
	return endpointPorts
}

func PortsEqualIgnoreOrder(a, b []*model.Port) (equal bool) {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]*model.Port)
	for _, portA := range a {
		aMap[portA.GetID()] = portA
	}

	for _, portB := range b {
		portA, found := aMap[portB.GetID()]
		if !found {
			return false
		}

		if !portB.Equals(portA) {
			return false
		}
	}
	return true
}

// DerivedName computes the "placeholder" name for an imported service
func DerivedName(namespace string, name string) string {
	hash := sha256.New()
	hash.Write([]byte(namespace + name))
	return "imported-" + strings.ToLower(base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(hash.Sum(nil)))[:10]
}

// CreateServiceImportStruct creates struct representation of a ServiceImport
func CreateServiceImportStruct(namespace string, name string, servicePorts []*model.Port) *v1alpha1.ServiceImport {
	serviceImportPorts := make([]v1alpha1.ServicePort, 0)
	for _, port := range servicePorts {
		serviceImportPorts = append(serviceImportPorts, PortToServiceImportPort(*port))
	}

	return &v1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: map[string]string{DerivedServiceAnnotation: DerivedName(namespace, name)},
		},
		Spec: v1alpha1.ServiceImportSpec{
			IPs:   []string{},
			Type:  v1alpha1.ClusterSetIP,
			Ports: serviceImportPorts,
		},
	}
}

// CreateDerivedServiceStruct creates struct representation of a derived service
func CreateDerivedServiceStruct(svcImport *v1alpha1.ServiceImport, importedSvcPorts []*model.Port) *v1.Service {
	ownerRef := metav1.NewControllerRef(svcImport, schema.GroupVersionKind{
		Version: svcImport.TypeMeta.APIVersion,
		Kind:    svcImport.TypeMeta.Kind,
	})

	svcPorts := make([]v1.ServicePort, 0)
	for _, svcPort := range importedSvcPorts {
		svcPorts = append(svcPorts, PortToServicePort(*svcPort))
	}

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       svcImport.Namespace,
			Name:            svcImport.Annotations[DerivedServiceAnnotation],
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeClusterIP,
			Ports: svcPorts,
		},
	}
}

func CreateEndpointForSlice(svc *v1.Service, ip string) discovery.Endpoint {
	t := true

	return discovery.Endpoint{
		Addresses: []string{ip},
		Conditions: discovery.EndpointConditions{
			Ready: &t,
		},
		TargetRef: &v1.ObjectReference{
			Kind:            "Service",
			Namespace:       svc.Namespace,
			Name:            svc.Name,
			UID:             svc.ObjectMeta.UID,
			ResourceVersion: svc.ObjectMeta.ResourceVersion,
		},
	}
}

func CreateEndpointSliceStruct(svc *v1.Service, svcImportName string) *discovery.EndpointSlice {
	return &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				discovery.LabelServiceName: svc.Name,      // derived Service name
				LabelServiceImportName:     svcImportName, // original ServiceImport name
			},
			GenerateName: svc.Name + "-",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(svc, schema.GroupVersionKind{
				Version: svc.TypeMeta.APIVersion,
				Kind:    svc.TypeMeta.Kind,
			})},
			Namespace: svc.Namespace,
		},
		AddressType: discovery.AddressTypeIPv4,
	}
}

// Function to support V1 endpoint mirroring

// PortToEndpointPort converts an internal model port to a k8s endpoint port
func portToV1EndpointPort(port model.Port) v1.EndpointPort {
	protocol := v1.Protocol(port.Protocol)
	return v1.EndpointPort{
		Name:     port.Name,
		Protocol: protocol,
		Port:     port.Port,
	}
}

// EndpointPortToPort converts a k8s v1 endpoint port to an internal model port
func v1EndpointPortToPort(port v1.EndpointPort) model.Port {
	return model.Port{
		Name:     port.Name,
		Port:     port.Port,
		Protocol: string(port.Protocol),
	}
}

func ipToV1EndpointAddress(ip string) v1.EndpointAddress {
	return v1.EndpointAddress{
		IP: ip,
	}
}

func EndpointsToV1EndpointSubsets(endpoints []*model.Endpoint) (subsets []v1.EndpointSubset) {
	endpointIPs := ExtractEndpointIPs(endpoints)
	endpointPorts := ExtractEndpointPorts(endpoints)

	addresses := make([]v1.EndpointAddress, 0)
	ports := make([]v1.EndpointPort, 0)

	for _, ip := range endpointIPs {
		address := ipToV1EndpointAddress(ip)
		addresses = append(addresses, address)
	}
	for _, port := range endpointPorts {
		port := portToV1EndpointPort(*port)
		ports = append(ports, port)
	}

	subsets = createV1EndpointSubsets(addresses, ports)
	return subsets
}

// ExtractV1EndpointIPs extracts all IP from V1 Endpoints
func ExtractV1EndpointIPs(subsets []v1.EndpointSubset) []string {
	ips := make([]string, 0)
	ipMap := make(map[string]string)
	if len(subsets) == 0 {
		return ips
	}
	if len(subsets[0].Addresses) == 0 {
		return ips
	}
	for _, address := range subsets[0].Addresses {
		ipMap[address.IP] = address.IP
	}
	for _, ip := range ipMap {
		ips = append(ips, ip)
	}
	return ips
}

// ExtractV1EndpointPorts extracts model port from v1 endpoints
func ExtractV1EndpointPorts(subsets []v1.EndpointSubset) []*model.Port {
	ports := make([]*model.Port, 0)
	if len(subsets) == 0 {
		return ports
	}
	for _, v1EndpointPort := range subsets[0].Ports {
		port := v1EndpointPortToPort(v1EndpointPort)
		ports = append(ports, &port)
	}
	return ports
}

func ExtractEndpointIPs(endpoints []*model.Endpoint) (endpointIPs []string) {
	uniqueIPs := make(map[string]string)
	for _, ep := range endpoints {
		uniqueIPs[ep.IP] = ep.IP
	}
	for _, endpointIP := range uniqueIPs {
		endpointIPs = append(endpointIPs, endpointIP)
	}
	return endpointIPs
}

func IPsEqualIgnoreOrder(a, b []string) (equal bool) {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]string)
	for _, ipA := range a {
		aMap[ipA] = ipA
	}

	for _, ipB := range b {
		ipA, found := aMap[ipB]
		if !found {
			return false
		}
		if ipB != ipA {
			return false
		}
	}
	return true
}

func createV1EndpointSubsets(addresses []v1.EndpointAddress, ports []v1.EndpointPort) (subsets []v1.EndpointSubset) {
	subset := v1.EndpointSubset{
		Addresses: addresses,
		Ports:     ports,
	}
	subsets = append(subsets, subset)
	return subsets
}

// CreateDerivedV1EndpointsStruct creates struct representation of a derived service
func CreateDerivedV1EndpointsStruct(svcImport *v1alpha1.ServiceImport, subsets []v1.EndpointSubset) *v1.Endpoints {
	ownerRef := metav1.NewControllerRef(svcImport, schema.GroupVersionKind{
		Version: svcImport.TypeMeta.APIVersion,
		Kind:    svcImport.TypeMeta.Kind,
	})

	return &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       svcImport.Namespace,
			Name:            svcImport.Annotations[DerivedServiceAnnotation],
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		Subsets: subsets,
	}
}
