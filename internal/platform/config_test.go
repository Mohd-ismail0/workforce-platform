package platform

import "testing"

func TestAuthConfigRequiresExplicitLocalMode(t *testing.T) {
	t.Setenv("AUTH_MODE", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected auth mode refusal")
	}
	t.Setenv("AUTH_MODE", "local")
	t.Setenv("LOCAL_AUTH_TOKENS", "token=org:user:requester")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthMode != "local" || c.Tokens["token"].OrgID != "org" {
		t.Fatalf("bad config %#v", c)
	}
}

func TestProductionRefusesLocalAuth(t *testing.T) {
	t.Setenv("AUTH_MODE", "local")
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOCAL_AUTH_TOKENS", "token=org:user:requester")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected production refusal")
	}
}
