/*
Copyright The Kubernetes Authors.

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

package proactivescaleup_test

import (
	"testing"

	"sigs.k8s.io/karpenter/pkg/controllers/proactivescaleup"
)

func TestFakePodLabel(t *testing.T) {
	// Verify that the FakePodLabel constant is set correctly
	expectedLabel := "karpenter.sh/fake-pod"
	if proactivescaleup.FakePodLabel != expectedLabel {
		t.Errorf("Expected FakePodLabel to be %q, got %q", expectedLabel, proactivescaleup.FakePodLabel)
	}
}

func TestInjectorCreation(t *testing.T) {
	// Test that we can create an injector without a client (nil is ok for this test)
	injector := proactivescaleup.NewInjector(nil)
	if injector == nil {
		t.Error("Expected NewInjector to return a non-nil injector")
	}
}
