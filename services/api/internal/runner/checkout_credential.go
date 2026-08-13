package runner

import (
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type CheckoutCredential struct {
	Partition               string    `json:"partition"`
	Scope                   string    `json:"scope"`
	ResourceID              string    `json:"resourceId"`
	RequestedScopes         []string  `json:"requestedScopes"`
	GrantedScopes           []string  `json:"grantedScopes"`
	Identity                string    `json:"identity"`
	Token                   string    `json:"token"`
	AuthenticationToken     string    `json:"authenticationToken"`
	AuthURL                 string    `json:"authUrl"`
	ExpiresAt               time.Time `json:"expiresAt"`
	AuthenticationExpiresAt time.Time `json:"authenticationExpiresAt"`
	ServicePurpose          string    `json:"servicePurpose"`
	Subject                 string    `json:"subject"`
	InsecureDevelopment     bool      `json:"insecureDevelopment,omitempty"`
}

type JobCredentials struct {
	JobToken
	Checkout *CheckoutCredential `json:"checkout,omitempty"`
}

func NewCheckoutCredential(value loreclient.Credential) CheckoutCredential {
	return CheckoutCredential{
		Partition: value.Partition, Scope: string(value.Scope), ResourceID: value.ResourceID,
		RequestedScopes: append([]string(nil), value.RequestedScopes...),
		GrantedScopes:   append([]string(nil), value.GrantedScopes...),
		Identity:        value.Identity, Token: value.Token, AuthenticationToken: value.AuthenticationToken,
		AuthURL: value.AuthURL, ExpiresAt: value.ExpiresAt,
		AuthenticationExpiresAt: value.AuthenticationExpiresAt,
		ServicePurpose:          value.Principal.ServicePurpose, Subject: value.Principal.Subject,
		InsecureDevelopment: value.InsecureDevelopment,
	}
}

func (credential CheckoutCredential) LoreCredential() loreclient.Credential {
	return loreclient.Credential{
		Partition: credential.Partition, Scope: loreclient.Scope(credential.Scope),
		ResourceID:      credential.ResourceID,
		RequestedScopes: append([]string(nil), credential.RequestedScopes...),
		GrantedScopes:   append([]string(nil), credential.GrantedScopes...),
		Identity:        credential.Identity, Subject: credential.Subject, Token: credential.Token,
		AuthenticationToken: credential.AuthenticationToken, AuthURL: credential.AuthURL,
		ExpiresAt: credential.ExpiresAt, AuthenticationExpiresAt: credential.AuthenticationExpiresAt,
		Principal:           loreclient.ServicePrincipal(credential.ServicePurpose, credential.Subject),
		InsecureDevelopment: credential.InsecureDevelopment,
	}
}
