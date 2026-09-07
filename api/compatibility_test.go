package api_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	alpha "thyris-sz/api/v1alpha1"
	beta "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func TestGraduatedCRDPreservesAlphaContract(t *testing.T) {
	read := func(path string) apiextensionsv1.CustomResourceDefinition {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(data, &crd); err != nil {
			t.Fatal(err)
		}
		return crd
	}
	old := read("testdata/v1alpha1-crd.yaml")
	current := read("../config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml")
	if current.Spec.Group != old.Spec.Group || !reflect.DeepEqual(current.Spec.Names, old.Spec.Names) || current.Spec.Scope != old.Spec.Scope {
		t.Fatal("graduation must preserve the resource identity and scope")
	}
	if current.Spec.Conversion != nil && current.Spec.Conversion.Strategy != apiextensionsv1.NoneConverter {
		t.Fatal("identical schemas must use the default None conversion strategy")
	}
	if len(current.Spec.Versions) != 2 {
		t.Fatalf("versions = %+v", current.Spec.Versions)
	}
	for _, version := range current.Spec.Versions {
		if !version.Served || version.Storage != (version.Name == "v1beta1") {
			t.Fatalf("served/storage flags for %s", version.Name)
		}
		if version.Name != "v1alpha1" && version.Name != "v1beta1" {
			t.Fatalf("unexpected version %s", version.Name)
		}
		if version.Deprecated != (version.Name == "v1alpha1") {
			t.Fatalf("deprecation flag for %s", version.Name)
		}
		if version.Deprecated && (version.DeprecationWarning == nil || *version.DeprecationWarning == "") {
			t.Fatal("alpha needs a migration warning")
		}
		if !reflect.DeepEqual(version.Schema, old.Spec.Versions[0].Schema) ||
			!reflect.DeepEqual(version.Subresources, old.Spec.Versions[0].Subresources) ||
			!reflect.DeepEqual(version.AdditionalPrinterColumns, old.Spec.Versions[0].AdditionalPrinterColumns) {
			t.Fatalf("%s changed the frozen schema, defaults, validation, status, or printer columns", version.Name)
		}
	}
}

func TestPolicyWireRoundTripAndScheme(t *testing.T) {
	data, err := os.ReadFile("testdata/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var original alpha.TSZGuardrailPolicy
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	var graduated beta.TSZGuardrailPolicy
	if err := json.Unmarshal(data, &graduated); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(graduated)
	if err != nil {
		t.Fatal(err)
	}
	var restored alpha.TSZGuardrailPolicy
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatal("spec/status/metadata lost across wire versions")
	}
	if original.Spec.ProcessingTimeoutOrDefault() != graduated.Spec.ProcessingTimeoutOrDefault() ||
		original.Spec.FailOpen() != graduated.Spec.FailOpen() ||
		original.Spec.StreamingMode() != graduated.Spec.StreamingMode() ||
		original.Spec.StreamingWindowBytes() != graduated.Spec.StreamingWindowBytes() {
		t.Fatal("beta changes runtime semantics")
	}
	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1alpha1", "v1beta1"} {
		gv := beta.GroupVersion
		gv.Version = version
		for _, kind := range []string{"TSZGuardrailPolicy", "TSZGuardrailPolicyList"} {
			if _, err := scheme.New(gv.WithKind(kind)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if preferred := scheme.PrioritizedVersionsForGroup(beta.GroupVersion.Group); len(preferred) != 2 || preferred[0] != beta.GroupVersion {
		t.Fatalf("scheme version priority = %v", preferred)
	}
}
