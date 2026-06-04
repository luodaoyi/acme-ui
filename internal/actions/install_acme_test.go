package actions

import "testing"

func TestBuildInstallAcmeRejectsBadEmail(t *testing.T) {
	if _, err := BuildInstallAcme(InstallAcmeRequest{Email: "bad", Confirm: true}); err == nil {
		t.Fatal("expected email validation error")
	}
}

func TestBuildInstallAcmeAllowsEmptyEmail(t *testing.T) {
	plan, err := BuildInstallAcme(InstallAcmeRequest{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Email != "" {
		t.Fatalf("unexpected email: %s", plan.Email)
	}
}

func TestBuildInstallAcmePlan(t *testing.T) {
	plan, err := BuildInstallAcme(InstallAcmeRequest{Email: "admin@example.com", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Email != "admin@example.com" {
		t.Fatalf("unexpected email: %s", plan.Email)
	}
	if plan.Preview == "" {
		t.Fatal("missing preview")
	}
}

func TestBuildInstallAcmeRequiresConfirm(t *testing.T) {
	if _, err := BuildInstallAcme(InstallAcmeRequest{Email: "admin@example.com"}); err == nil {
		t.Fatal("expected confirmation error")
	}
}
