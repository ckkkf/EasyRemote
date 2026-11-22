package adminapi

import "testing"

func TestPermissionsReadonlyCannotWrite(t *testing.T) {
	ro := Permissions(RoleReadonly)
	for _, action := range ro {
		if action == "user.ban" || action == "session.kill" || action == "config.write" {
			t.Fatalf("readonly must not have %s", action)
		}
	}
	super := Permissions(RoleSuper)
	found := false
	for _, action := range super {
		if action == "config.write" {
			found = true
		}
	}
	if !found {
		t.Fatal("super must have config.write")
	}
}
