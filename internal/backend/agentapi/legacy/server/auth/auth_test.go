package auth

import (
	"testing"
)

func TestSessionStore(t *testing.T) {
	t.Run("Create and Get", func(t *testing.T) {
		store := NewSessionStore("test-secret")

		user := &AuthUser{
			ID:    "user1",
			Email: "user1@example.com",
			Name:  "User One",
		}

		token, err := store.Create(user)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if token == "" {
			t.Error("Expected non-empty token")
		}

		retrieved, ok := store.Get(token)
		if !ok {
			t.Fatal("Expected to retrieve user")
		}

		if retrieved.ID != user.ID {
			t.Errorf("Expected ID %s, got %s", user.ID, retrieved.ID)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		store := NewSessionStore("test-secret")

		user := &AuthUser{ID: "user1"}
		token, _ := store.Create(user)

		store.Delete(token)

		if _, ok := store.Get(token); ok {
			t.Error("Expected session to be deleted")
		}
	})
}

func TestTokenAuthAdapter(t *testing.T) {
	t.Run("NoAuthToken", func(t *testing.T) {
		t.Setenv("AUTH_TOKEN", "")

		adapter := NewTokenAuthAdapter()

		if adapter.authToken != "" {
			t.Error("Expected empty auth token")
		}
	})

	t.Run("WithAuthToken", func(t *testing.T) {
		t.Setenv("AUTH_TOKEN", "test-token-123")

		adapter := NewTokenAuthAdapter()

		if adapter.authToken != "test-token-123" {
			t.Errorf("Expected auth token 'test-token-123', got %s", adapter.authToken)
		}
	})

	t.Run("AdminUsers", func(t *testing.T) {
		t.Setenv("ADMIN_USERS", "admin@example.com,superuser@example.com")

		adapter := NewTokenAuthAdapter()

		if !adapter.adminUsers["admin@example.com"] {
			t.Error("Expected admin@example.com to be admin")
		}

		if !adapter.adminUsers["superuser@example.com"] {
			t.Error("Expected superuser@example.com to be admin")
		}

		if adapter.adminUsers["user@example.com"] {
			t.Error("Expected user@example.com to not be admin")
		}
	})
}

func TestGenerateRandomString(t *testing.T) {
	t.Run("Length", func(t *testing.T) {
		str := generateRandomString(32)
		if len(str) != 32 {
			t.Errorf("Expected length 32, got %d", len(str))
		}
	})

	t.Run("Uniqueness", func(t *testing.T) {
		str1 := generateRandomString(32)
		str2 := generateRandomString(32)

		if str1 == str2 {
			t.Error("Expected different random strings")
		}
	})
}
