package server

import (
	"testing"
	"time"
)

func TestScrollbackBuffer(t *testing.T) {
	t.Run("Write and Read", func(t *testing.T) {
		buf := NewScrollbackBuffer(100)

		data := []byte("hello world")
		n, err := buf.Write(data)
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if n != len(data) {
			t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
		}

		result := buf.Read()
		if string(result) != string(data) {
			t.Errorf("Expected %q, got %q", data, result)
		}
	})

	t.Run("Truncation", func(t *testing.T) {
		buf := NewScrollbackBuffer(10)

		buf.Write([]byte("12345"))
		buf.Write([]byte("67890"))
		buf.Write([]byte("ABCDE"))

		result := buf.Read()
		if len(result) > 10 {
			t.Errorf("Buffer exceeded max size: %d > 10", len(result))
		}

		// Should contain the most recent data
		if string(result) != "67890ABCDE" {
			t.Errorf("Expected truncated buffer to contain recent data, got %q", result)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		buf := NewScrollbackBuffer(100)
		buf.Write([]byte("test"))
		buf.Clear()

		if buf.Size() != 0 {
			t.Errorf("Expected size 0 after clear, got %d", buf.Size())
		}
	})
}

func TestSessionStore(t *testing.T) {
	t.Run("Add and Get", func(t *testing.T) {
		store := NewSessionStore(5000, 1024)

		entry := store.Add("token1", "user1", nil)
		if entry.Token != "token1" {
			t.Errorf("Expected token1, got %s", entry.Token)
		}
		if entry.UserID != "user1" {
			t.Errorf("Expected user1, got %s", entry.UserID)
		}

		retrieved := store.Get("token1")
		if retrieved == nil {
			t.Fatal("Expected to retrieve entry")
		}
		if retrieved.Token != "token1" {
			t.Errorf("Expected token1, got %s", retrieved.Token)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		store := NewSessionStore(5000, 1024)
		store.Add("token1", "user1", nil)

		store.Delete("token1")

		if store.Get("token1") != nil {
			t.Error("Expected entry to be deleted")
		}
	})

	t.Run("ListByUser", func(t *testing.T) {
		store := NewSessionStore(5000, 1024)
		store.Add("token1", "user1", nil)
		store.Add("token2", "user1", nil)
		store.Add("token3", "user2", nil)

		user1Sessions := store.ListByUser("user1")
		if len(user1Sessions) != 2 {
			t.Errorf("Expected 2 sessions for user1, got %d", len(user1Sessions))
		}

		user2Sessions := store.ListByUser("user2")
		if len(user2Sessions) != 1 {
			t.Errorf("Expected 1 session for user2, got %d", len(user2Sessions))
		}
	})

	t.Run("CountByUser", func(t *testing.T) {
		store := NewSessionStore(5000, 1024)
		store.Add("token1", "user1", nil)
		store.Add("token2", "user1", nil)

		count := store.CountByUser("user1")
		if count != 2 {
			t.Errorf("Expected count 2, got %d", count)
		}
	})

	t.Run("GracePeriod", func(t *testing.T) {
		store := NewSessionStore(100, 1024) // 100ms grace period
		store.Add("token1", "user1", nil)

		expired := false
		store.StartGrace("token1", func() {
			expired = true
		})

		entry := store.Get("token1")
		if !entry.InGracePeriod {
			t.Error("Expected entry to be in grace period")
		}

		time.Sleep(150 * time.Millisecond)

		if !expired {
			t.Error("Expected grace period to expire")
		}

		if store.Get("token1") != nil {
			t.Error("Expected entry to be deleted after grace period")
		}
	})

	t.Run("CancelGrace", func(t *testing.T) {
		store := NewSessionStore(100, 1024)
		store.Add("token1", "user1", nil)

		expired := false
		store.StartGrace("token1", func() {
			expired = true
		})

		store.CancelGrace("token1")

		time.Sleep(150 * time.Millisecond)

		if expired {
			t.Error("Expected grace period to be cancelled")
		}

		entry := store.Get("token1")
		if entry == nil {
			t.Error("Expected entry to still exist")
		}
		if entry.InGracePeriod {
			t.Error("Expected entry to not be in grace period")
		}
	})
}

func TestUserHourlyRateLimiter(t *testing.T) {
	t.Run("Allow and Record", func(t *testing.T) {
		limiter := NewUserHourlyRateLimiter(3)

		if !limiter.Allow("user1") {
			t.Error("Expected to allow first attempt")
		}

		limiter.Record("user1")
		limiter.Record("user1")
		limiter.Record("user1")

		if limiter.Allow("user1") {
			t.Error("Expected to deny after limit reached")
		}
	})

	t.Run("RetryAfterSeconds", func(t *testing.T) {
		limiter := NewUserHourlyRateLimiter(1)

		limiter.Record("user1")

		retryAfter := limiter.RetryAfterSeconds("user1")
		if retryAfter <= 0 || retryAfter > 3600 {
			t.Errorf("Expected retry after between 0 and 3600, got %d", retryAfter)
		}
	})
}

func TestSessionManager(t *testing.T) {
	spawner := func(cols, rows int, userID string) (interface{}, error) {
		return &struct{}{}, nil
	}

	t.Run("Create", func(t *testing.T) {
		manager := NewSessionManager(10, spawner, 5000, 1024, 3, 10)

		err := manager.Create("token1", 80, 24, "user1")
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if manager.ActiveCount() != 1 {
			t.Errorf("Expected 1 active session, got %d", manager.ActiveCount())
		}
	})

	t.Run("UserConcurrentLimit", func(t *testing.T) {
		manager := NewSessionManager(10, spawner, 5000, 1024, 2, 10)

		manager.Create("token1", 80, 24, "user1")
		manager.Create("token2", 80, 24, "user1")

		err := manager.Create("token3", 80, 24, "user1")
		if err == nil {
			t.Error("Expected error when exceeding user limit")
		}
	})

	t.Run("ServerCapacity", func(t *testing.T) {
		manager := NewSessionManager(2, spawner, 5000, 1024, 10, 10)

		manager.Create("token1", 80, 24, "user1")
		manager.Create("token2", 80, 24, "user2")

		err := manager.Create("token3", 80, 24, "user3")
		if err == nil {
			t.Error("Expected error when server at capacity")
		}
	})

	t.Run("Reconnect", func(t *testing.T) {
		manager := NewSessionManager(10, spawner, 5000, 1024, 3, 10)

		manager.Create("token1", 80, 24, "user1")

		entry, err := manager.Reconnect("token1")
		if err != nil {
			t.Fatalf("Reconnect failed: %v", err)
		}
		if entry.Token != "token1" {
			t.Errorf("Expected token1, got %s", entry.Token)
		}
	})
}

func TestUserStore(t *testing.T) {
	t.Run("Acquire and Release", func(t *testing.T) {
		store := NewUserStore()

		store.Acquire("user1")
		if store.Count("user1") != 1 {
			t.Errorf("Expected count 1, got %d", store.Count("user1"))
		}

		store.Acquire("user1")
		if store.Count("user1") != 2 {
			t.Errorf("Expected count 2, got %d", store.Count("user1"))
		}

		store.Release("user1")
		if store.Count("user1") != 1 {
			t.Errorf("Expected count 1 after release, got %d", store.Count("user1"))
		}

		store.Release("user1")
		if store.Count("user1") != 0 {
			t.Errorf("Expected count 0 after full release, got %d", store.Count("user1"))
		}
	})

	t.Run("TotalUsers", func(t *testing.T) {
		store := NewUserStore()

		store.Acquire("user1")
		store.Acquire("user2")
		store.Acquire("user3")

		if store.TotalUsers() != 3 {
			t.Errorf("Expected 3 total users, got %d", store.TotalUsers())
		}

		store.Release("user2")

		if store.TotalUsers() != 2 {
			t.Errorf("Expected 2 total users after release, got %d", store.TotalUsers())
		}
	})
}

func TestConnectionRateLimiter(t *testing.T) {
	t.Run("Allow", func(t *testing.T) {
		limiter := NewConnectionRateLimiter(3, 1000)

		if !limiter.Allow("192.168.1.1") {
			t.Error("Expected to allow first connection")
		}
		if !limiter.Allow("192.168.1.1") {
			t.Error("Expected to allow second connection")
		}
		if !limiter.Allow("192.168.1.1") {
			t.Error("Expected to allow third connection")
		}
		if limiter.Allow("192.168.1.1") {
			t.Error("Expected to deny fourth connection")
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		limiter := NewConnectionRateLimiter(1, 50)

		limiter.Allow("192.168.1.1")

		time.Sleep(100 * time.Millisecond)

		limiter.Cleanup()

		if !limiter.Allow("192.168.1.1") {
			t.Error("Expected to allow after cleanup")
		}
	})
}
