package types

import "time"

// NotificationType represents the type of notification.
type NotificationType string

const (
	NotificationTypeInfo    NotificationType = "info"
	NotificationTypeWarning NotificationType = "warning"
	NotificationTypeError   NotificationType = "error"
	NotificationTypeSuccess NotificationType = "success"
)

// NotificationPriority represents the priority of a notification.
type NotificationPriority string

const (
	NotificationPriorityLow    NotificationPriority = "low"
	NotificationPriorityNormal NotificationPriority = "normal"
	NotificationPriorityHigh   NotificationPriority = "high"
	NotificationPriorityUrgent NotificationPriority = "urgent"
)

// Notification represents a notification message.
type Notification struct {
	ID        string                 `json:"id"`
	Type      NotificationType       `json:"type"`
	Priority  NotificationPriority   `json:"priority"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Read      bool                   `json:"read"`
	Dismissed bool                   `json:"dismissed"`
	Actions   []NotificationAction   `json:"actions,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt *time.Time             `json:"expires_at,omitempty"`
}

// NotificationAction represents an action that can be taken on a notification.
type NotificationAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Action  string `json:"action"`
	Primary bool   `json:"primary,omitempty"`
}

// NotificationChannel represents a channel for sending notifications.
type NotificationChannel string

const (
	NotificationChannelInApp   NotificationChannel = "in_app"
	NotificationChannelTelegram NotificationChannel = "telegram"
	NotificationChannelDiscord NotificationChannel = "discord"
	NotificationChannelSlack   NotificationChannel = "slack"
	NotificationChannelEmail   NotificationChannel = "email"
)

// NotificationConfig represents notification configuration.
type NotificationConfig struct {
	Enabled  bool                  `json:"enabled"`
	Channels []NotificationChannel `json:"channels"`
	MinPriority NotificationPriority `json:"min_priority,omitempty"`
	Telegram *TelegramConfig       `json:"telegram,omitempty"`
	Discord  *DiscordConfig        `json:"discord,omitempty"`
	Slack    *SlackConfig          `json:"slack,omitempty"`
	Email    *EmailConfig          `json:"email,omitempty"`
}

// TelegramConfig represents Telegram notification configuration.
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// DiscordConfig represents Discord notification configuration.
type DiscordConfig struct {
	WebhookURL string `json:"webhook_url"`
}

// SlackConfig represents Slack notification configuration.
type SlackConfig struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel,omitempty"`
}

// EmailConfig represents email notification configuration.
type EmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// NewNotification creates a new notification.
func NewNotification(notifType NotificationType, title, message string) *Notification {
	return &Notification{
		ID:        UUID(),
		Type:      notifType,
		Priority:  NotificationPriorityNormal,
		Title:     title,
		Message:   message,
		Timestamp: time.Now().UTC(),
		Metadata:  make(map[string]interface{}),
	}
}

// NewInfoNotification creates a new info notification.
func NewInfoNotification(title, message string) *Notification {
	return NewNotification(NotificationTypeInfo, title, message)
}

// NewWarningNotification creates a new warning notification.
func NewWarningNotification(title, message string) *Notification {
	notif := NewNotification(NotificationTypeWarning, title, message)
	notif.Priority = NotificationPriorityHigh
	return notif
}

// NewErrorNotification creates a new error notification.
func NewErrorNotification(title, message string) *Notification {
	notif := NewNotification(NotificationTypeError, title, message)
	notif.Priority = NotificationPriorityUrgent
	return notif
}

// NewSuccessNotification creates a new success notification.
func NewSuccessNotification(title, message string) *Notification {
	return NewNotification(NotificationTypeSuccess, title, message)
}

// WithPriority sets the priority of the notification.
func (n *Notification) WithPriority(priority NotificationPriority) *Notification {
	n.Priority = priority
	return n
}

// WithAction adds an action to the notification.
func (n *Notification) WithAction(id, label, action string, primary bool) *Notification {
	n.Actions = append(n.Actions, NotificationAction{
		ID:      id,
		Label:   label,
		Action:  action,
		Primary: primary,
	})
	return n
}

// WithMetadata adds metadata to the notification.
func (n *Notification) WithMetadata(key string, value interface{}) *Notification {
	if n.Metadata == nil {
		n.Metadata = make(map[string]interface{})
	}
	n.Metadata[key] = value
	return n
}

// WithExpiration sets an expiration time for the notification.
func (n *Notification) WithExpiration(expiresAt time.Time) *Notification {
	n.ExpiresAt = &expiresAt
	return n
}

// MarkRead marks the notification as read.
func (n *Notification) MarkRead() {
	n.Read = true
}

// Dismiss dismisses the notification.
func (n *Notification) Dismiss() {
	n.Dismissed = true
}

// IsExpired returns true if the notification has expired.
func (n *Notification) IsExpired() bool {
	if n.ExpiresAt == nil {
		return false
	}
	return time.Now().UTC().After(*n.ExpiresAt)
}
