package monitorgen_test

import (
	"testing"

	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/monitorgen"
	"github.com/hamza-m-masood/camunda-helm-toolkit/internal/rules"
)

func TestDiscoverServices_ExtractsNamedPortsOnly(t *testing.T) {
	docs := []rules.ManifestDoc{
		{
			Kind: "Service",
			Name: "release-zeebe",
			Text: `
kind: Service
metadata:
  name: release-zeebe
  labels:
    app.kubernetes.io/component: zeebe-broker
spec:
  selector:
    app.kubernetes.io/component: zeebe-broker
  ports:
    - name: gateway
      port: 26500
    - name: server
      port: 9600
    - port: 8080
`,
		},
		{Kind: "ConfigMap", Name: "irrelevant", Text: "kind: ConfigMap\nmetadata:\n  name: irrelevant\n"},
	}

	services := monitorgen.DiscoverServices(docs)
	if len(services) != 1 {
		t.Fatalf("expected 1 Service discovered, got %d", len(services))
	}
	s := services[0]
	if s.Name != "release-zeebe" {
		t.Errorf("Name = %s", s.Name)
	}
	if len(s.Ports) != 2 || s.Ports[0] != "gateway" || s.Ports[1] != "server" {
		t.Errorf("expected named ports [gateway server], got %v (the unnamed port 8080 must be excluded)", s.Ports)
	}

	port, ok := monitorgen.PickMetricsPort(s)
	if !ok || port != "server" {
		t.Errorf("PickMetricsPort = %q, %v; want \"server\", true", port, ok)
	}
}

func TestPickMetricsPort_NoRecognizedPort_ReturnsFalse(t *testing.T) {
	s := monitorgen.ServiceInfo{Name: "x", Ports: []string{"gateway", "grpc"}}
	if _, ok := monitorgen.PickMetricsPort(s); ok {
		t.Error("expected PickMetricsPort to refuse to guess when no known port name is present")
	}
}
