package management

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"golang.org/x/crypto/bcrypt"
)

func TestUserUpdateFromRequestHashesManagementPassword(t *testing.T) {
	t.Parallel()

	password := "plain-management-password"
	update, ok := userUpdateFromRequest(nil, userWriteRequest{Password: &password}, false)
	if !ok {
		t.Fatalf("userUpdateFromRequest() ok = false, want true")
	}
	if update.Password == nil {
		t.Fatalf("UserUpdate.Password = nil, want hash")
	}
	if *update.Password == password {
		t.Fatalf("UserUpdate.Password stored plaintext")
	}
	if errCompare := bcrypt.CompareHashAndPassword([]byte(*update.Password), []byte(password)); errCompare != nil {
		t.Fatalf("stored password is not a valid bcrypt hash: %v", errCompare)
	}
}

func TestUserUpdateFromRequestKeepsExistingBcryptPassword(t *testing.T) {
	t.Parallel()

	rawHash, errHash := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	if errHash != nil {
		t.Fatalf("GenerateFromPassword() error = %v", errHash)
	}
	password := string(rawHash)
	update, ok := userUpdateFromRequest(nil, userWriteRequest{Password: &password}, false)
	if !ok {
		t.Fatalf("userUpdateFromRequest() ok = false, want true")
	}
	if update.Password == nil || *update.Password != password {
		t.Fatalf("UserUpdate.Password = %q, want existing hash", stringValue(update.Password))
	}
}

func TestUserRecordToMapDoesNotExposePassword(t *testing.T) {
	t.Parallel()

	item := userRecordToMap(&cluster.UserRecord{Password: "stored-secret"})
	if _, ok := item["password"]; ok {
		t.Fatalf("userRecordToMap exposed password")
	}
	if got := item["password_set"]; got != true {
		t.Fatalf("password_set = %v, want true", got)
	}

	empty := userRecordToMap(&cluster.UserRecord{})
	if got := empty["password_set"]; got != false {
		t.Fatalf("empty password_set = %v, want false", got)
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
