package apiserver

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	configclientfake "github.com/openshift/client-go/config/clientset/versioned/fake"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestValidateSNINames(t *testing.T) {
	expectNoErrors := func(t *testing.T, errs field.ErrorList) {
		t.Helper()
		if len(errs) > 0 {
			t.Fatal(errs)
		}
	}

	tests := []struct {
		name string

		internalName string
		apiserver    *configv1.APIServer

		validateErrors func(t *testing.T, errs field.ErrorList)
	}{
		{
			name:           "no sni",
			internalName:   "internal.host.com",
			apiserver:      &configv1.APIServer{},
			validateErrors: expectNoErrors,
		},
		{
			name:         "allowed sni",
			internalName: "internal.host.com",
			apiserver: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					ServingCerts: configv1.APIServerServingCerts{
						NamedCertificates: []configv1.APIServerNamedServingCert{
							{
								Names: []string{"external.host.com", "somwhere.else.*"},
							},
						},
					},
				},
			},
			validateErrors: expectNoErrors,
		},
		{
			name:         "directly invalid sni",
			internalName: "internal.host.com",
			apiserver: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					ServingCerts: configv1.APIServerServingCerts{
						NamedCertificates: []configv1.APIServerNamedServingCert{
							{Names: []string{"external.host.com", "somwhere.else.*"}},
							{Names: []string{"foo.bar", "internal.host.com"}},
						},
					},
				},
			},
			validateErrors: func(t *testing.T, errs field.ErrorList) {
				t.Helper()
				if len(errs) != 1 {
					t.Fatal(errs)
				}
				if errs[0].Error() != `spec.servingCerts[1].names[1]: Invalid value: "internal.host.com": may not match internal loadbalancer: "internal.host.com"` {
					t.Error(errs[0])
				}
			},
		},
		{
			name:         "wildcard invalid sni",
			internalName: "internal.host.com",
			apiserver: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					ServingCerts: configv1.APIServerServingCerts{
						NamedCertificates: []configv1.APIServerNamedServingCert{
							{Names: []string{"internal.*"}},
						},
					},
				},
			},
			validateErrors: func(t *testing.T, errs field.ErrorList) {
				t.Helper()
				if len(errs) != 1 {
					t.Fatal(errs)
				}
				if errs[0].Error() != `spec.servingCerts[0].names[0]: Invalid value: "internal.*": may not match internal loadbalancer: "internal.host.com"` {
					t.Error(errs[0])
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeclient := configclientfake.NewSimpleClientset(&configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Status: configv1.InfrastructureStatus{
					APIServerInternalURL: test.internalName,
				},
			})

			instance := apiserverV1{
				infrastructureGetter: func() configv1client.InfrastructuresGetter {
					return fakeclient.ConfigV1()
				},
			}
			test.validateErrors(t, instance.validateSNINames(test.apiserver))
		})

	}
}

func Test_validateTLSSecurityProfile(t *testing.T) {
	rootFieldPath := field.NewPath("testSpec")

	tests := []struct {
		name    string
		profile *configv1.TLSSecurityProfile
		want    field.ErrorList
	}{
		{
			name:    "nil profile",
			profile: nil,
			want:    field.ErrorList{},
		},
		{
			name:    "empty profile",
			profile: &configv1.TLSSecurityProfile{},
			want:    field.ErrorList{},
		},
		{
			name: "type does not match set field",
			profile: &configv1.TLSSecurityProfile{
				Type:   configv1.TLSProfileIntermediateType,
				Modern: &configv1.ModernTLSProfile{},
			},
			want: field.ErrorList{
				field.Required(rootFieldPath.Child("intermediate"), "type set to Intermediate, but the corresponding field is unset"),
			},
		},
		{
			name: "unknown type",
			profile: &configv1.TLSSecurityProfile{
				Type: "something",
			},
			want: field.ErrorList{
				field.Invalid(rootFieldPath.Child("type"), "something", "unknown type, valid values are: [Old Intermediate Modern Custom]"),
			},
		},
		{
			name: "unknown cipher",
			profile: &configv1.TLSSecurityProfile{
				Type: "Custom",
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						Ciphers: []string{
							"UNKNOWN_CIPHER",
						},
					},
				},
			},
			want: field.ErrorList{
				field.Invalid(rootFieldPath.Child("custom", "ciphers"), []string{"UNKNOWN_CIPHER"}, "no supported cipher suite found"),
			},
		},
		{
			name: "unknown cipher but a tls1.3 cipher",
			profile: &configv1.TLSSecurityProfile{
				Type: "Custom",
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						Ciphers: []string{
							"UNKNOWN_CIPHER", "TLS_CHACHA20_POLY1305_SHA256",
						},
					},
				},
			},
			want: field.ErrorList{},
		},
		{
			name: "unknown cipher but a normal cipher",
			profile: &configv1.TLSSecurityProfile{
				Type: "Custom",
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						Ciphers: []string{
							"UNKNOWN_CIPHER", "ECDHE-ECDSA-CHACHA20-POLY1305",
						},
					},
				},
			},
			want: field.ErrorList{},
		},
		{
			name: "no ciphers in custom profile",
			profile: &configv1.TLSSecurityProfile{
				Type: "Custom",
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{},
				},
			},
			want: field.ErrorList{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateTLSSecurityProfile(rootFieldPath, tt.profile)

			if len(tt.want) != len(got) {
				t.Errorf("expected %d errors, got %d: %v", len(tt.want), len(got), got)
				return
			}

			for i, err := range got {
				if err.Error() != tt.want[i].Error() {
					t.Errorf("expected %v, got %v", tt.want, got)
					break
				}
			}
		})
	}
}
