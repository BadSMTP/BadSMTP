package server

import "testing"

func TestDiscardMessageStore(t *testing.T) {
	store := NewDiscardMessageStore()
	msg := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient@example.com"},
		Content: "Subject: Test\r\n\r\nBody",
		Size:    10,
	}
	if err := store.Store(msg); err != nil {
		t.Fatalf("DiscardMessageStore.Store returned error: %v", err)
	}
}

func TestDefaultStoreIsDiscardWithoutMailbox(t *testing.T) {
	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	if _, ok := cfg.MessageStore.(*DiscardMessageStore); !ok {
		t.Fatalf("expected DiscardMessageStore when no mailbox is set, got %T", cfg.MessageStore)
	}
}

func TestDefaultStoreIsFileWithMailbox(t *testing.T) {
	cfg := &Config{Port: 2525, MailboxDir: t.TempDir()}
	cfg.EnsureDefaults()
	if _, ok := cfg.MessageStore.(*DefaultMessageStore); !ok {
		t.Fatalf("expected DefaultMessageStore when mailbox is set, got %T", cfg.MessageStore)
	}
}
