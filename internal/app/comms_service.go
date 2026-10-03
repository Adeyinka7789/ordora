package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// Notification kinds for the in-app feed.
const (
	NotifKindComplaintReply   = "complaint_reply"
	NotifKindComplaintResolve = "complaint_resolved"
	NotifKindBroadcast        = "broadcast"
)

// Complaint statuses.
const (
	ComplaintOpen     = "OPEN"
	ComplaintResolved = "RESOLVED"
)

// Notification is one in-app feed row.
type Notification struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Kind      string
	Title     string
	Body      string
	Link      string
	BatchID   *uuid.UUID
	IsRead    bool
	ReadAt    *time.Time
	CreatedAt time.Time
}

// Complaint is a user-filed support request.
type Complaint struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	UserID     uuid.UUID
	Subject    string
	Message    string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ResolvedAt *time.Time
}

// ComplaintReply is one message in a complaint thread.
type ComplaintReply struct {
	ID            uuid.UUID
	ComplaintID   uuid.UUID
	OrgID         uuid.UUID
	AuthorUserID  *uuid.UUID
	AuthorAdminID *uuid.UUID
	Body          string
	CreatedAt     time.Time
}

// ComplaintReplyView is a reply plus its display author.
type ComplaintReplyView struct {
	Reply      ComplaintReply
	AuthorName string
	IsAdmin    bool
}

// AdminComplaintRow is a complaint plus cross-tenant display fields.
type AdminComplaintRow struct {
	Complaint   Complaint
	UserName    string
	UserEmail   string
	OrgName     string
	ReplyCount  int
	LastReplyAt *time.Time
}

// BroadcastSummary groups one admin broadcast batch.
type BroadcastSummary struct {
	BatchID    uuid.UUID
	Title      string
	Body       string
	Link       string
	Recipients int
	CreatedAt  time.Time
}

// CommsTxRunner runs tenant-scoped or plain transactions.
type CommsTxRunner interface {
	WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error
	WithTx(ctx context.Context, fn func(pgx.Tx) error) error
}

// NotificationStore persists the in-app feed.
type NotificationStore interface {
	CreateTx(ctx context.Context, tx pgx.Tx, n *Notification) error
	BroadcastTx(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, title, body, link string) (int64, error)
	ListForUserTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, limit, offset int) ([]Notification, int, error)
	CountUnreadTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (int, error)
	MarkReadTx(ctx context.Context, tx pgx.Tx, userID, notifID uuid.UUID, now time.Time) error
	MarkAllReadTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) error
	ListBroadcastsTx(ctx context.Context, tx pgx.Tx, query string, limit, offset int) ([]BroadcastSummary, int, error)
}

// ComplaintStore persists complaints and their threads.
type ComplaintStore interface {
	CreateTx(ctx context.Context, tx pgx.Tx, c *Complaint) error
	ListForUserTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, limit, offset int) ([]Complaint, int, error)
	GetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Complaint, error)
	AddReplyTx(ctx context.Context, tx pgx.Tx, r *ComplaintReply) error
	ListRepliesTx(ctx context.Context, tx pgx.Tx, complaintID uuid.UUID) ([]ComplaintReplyView, error)
	SetStatusTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, now time.Time) error
	AdminListTx(ctx context.Context, tx pgx.Tx, query, status string, limit, offset int) ([]AdminComplaintRow, int, error)
	AdminGetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*AdminComplaintRow, error)
}

// Errors.
var (
	ErrCommsTitleRequired   = errors.New("comms: title is required")
	ErrCommsTitleTooLong    = errors.New("comms: title is too long (max 160)")
	ErrCommsBodyTooLong     = errors.New("comms: message is too long")
	ErrCommsSubjectRequired = errors.New("comms: subject is required")
	ErrComplaintNotFound    = errors.New("comms: complaint not found")
	ErrComplaintClosed      = errors.New("comms: complaint is resolved")
	ErrNotifNotFound        = errors.New("comms: notification not found")
	ErrCommsStatusInvalid   = errors.New("comms: invalid status")
)

// CommsService orchestrates in-app notifications, complaints, and broadcasts.
type CommsService struct {
	db              CommsTxRunner
	adminDB         CommsTxRunner
	notifs          NotificationStore
	complaints      ComplaintStore
	adminNotifs     NotificationStore
	adminComplaints ComplaintStore
	adminAudit      AdminAuditWriter
	ids             IDGen
	now             func() time.Time
}

// CommsServiceDeps bundles the dependencies. AdminDB/AdminNotifs/
// AdminComplaints use the BYPASSRLS admin connection for cross-tenant work.
type CommsServiceDeps struct {
	DB              CommsTxRunner
	AdminDB         CommsTxRunner
	Notifs          NotificationStore
	Complaints      ComplaintStore
	AdminNotifs     NotificationStore
	AdminComplaints ComplaintStore
	Audit           AdminAuditWriter
	IDs             IDGen
	Now             func() time.Time
}

func NewCommsService(d CommsServiceDeps) *CommsService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &CommsService{
		db:              d.DB,
		adminDB:         d.AdminDB,
		notifs:          d.Notifs,
		complaints:      d.Complaints,
		adminNotifs:     d.AdminNotifs,
		adminComplaints: d.AdminComplaints,
		adminAudit:      d.Audit,
		ids:             d.IDs,
		now:             d.Now,
	}
}

// -----------------------------------------------------------------------------
// Tenant: notifications
// -----------------------------------------------------------------------------

// ListNotifications returns the user's feed page, newest first.
func (s *CommsService) ListNotifications(ctx context.Context, scope tenant.TenantScope, limit, offset int) ([]Notification, int, error) {
	var out []Notification
	var total int
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		rows, n, err := s.notifs.ListForUserTx(ctx, tx, scope.UserID, limit, offset)
		if err != nil {
			return err
		}
		out, total = rows, n
		return nil
	})
	return out, total, err
}

// UnreadCount returns the number of unread feed rows.
func (s *CommsService) UnreadCount(ctx context.Context, scope tenant.TenantScope) (int, error) {
	var n int
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		var err error
		n, err = s.notifs.CountUnreadTx(ctx, tx, scope.UserID)
		return err
	})
	return n, err
}

// MarkNotificationRead marks one row read (idempotent).
func (s *CommsService) MarkNotificationRead(ctx context.Context, scope tenant.TenantScope, notifID uuid.UUID) error {
	return s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return s.notifs.MarkReadTx(ctx, tx, scope.UserID, notifID, s.now())
	})
}

// MarkAllNotificationsRead marks the whole feed read.
func (s *CommsService) MarkAllNotificationsRead(ctx context.Context, scope tenant.TenantScope) error {
	return s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return s.notifs.MarkAllReadTx(ctx, tx, scope.UserID, s.now())
	})
}

// -----------------------------------------------------------------------------
// Tenant: complaints
// -----------------------------------------------------------------------------

// CreateComplaint files a new support request.
func (s *CommsService) CreateComplaint(ctx context.Context, scope tenant.TenantScope, subject, message string) (*Complaint, error) {
	subject = strings.TrimSpace(subject)
	message = strings.TrimSpace(message)
	if subject == "" {
		return nil, ErrCommsSubjectRequired
	}
	if len(subject) > 160 {
		return nil, ErrCommsTitleTooLong
	}
	if message == "" {
		return nil, ErrCommsSubjectRequired
	}
	if len(message) > 4000 {
		return nil, ErrCommsBodyTooLong
	}
	now := s.now()
	c := &Complaint{
		ID:        s.ids.New(),
		OrgID:     scope.OrgID,
		UserID:    scope.UserID,
		Subject:   subject,
		Message:   message,
		Status:    ComplaintOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return s.complaints.CreateTx(ctx, tx, c)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListComplaints returns the user's complaints, newest first.
func (s *CommsService) ListComplaints(ctx context.Context, scope tenant.TenantScope, limit, offset int) ([]Complaint, int, error) {
	var out []Complaint
	var total int
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		rows, n, err := s.complaints.ListForUserTx(ctx, tx, scope.UserID, limit, offset)
		if err != nil {
			return err
		}
		out, total = rows, n
		return nil
	})
	return out, total, err
}

// ComplaintThread is a complaint plus its replies for the owner.
type ComplaintThread struct {
	Complaint Complaint
	Replies   []ComplaintReplyView
}

// GetComplaintThread loads a complaint owned by the caller plus its thread.
func (s *CommsService) GetComplaintThread(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*ComplaintThread, error) {
	var thread *ComplaintThread
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		c, err := s.complaints.GetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if c.UserID != scope.UserID {
			return ErrComplaintNotFound
		}
		replies, err := s.complaints.ListRepliesTx(ctx, tx, id)
		if err != nil {
			return err
		}
		thread = &ComplaintThread{Complaint: *c, Replies: replies}
		return nil
	})
	return thread, err
}

// ReplyToComplaint appends a user follow-up. Only on OPEN complaints.
func (s *CommsService) ReplyToComplaint(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return ErrCommsSubjectRequired
	}
	if len(body) > 4000 {
		return ErrCommsBodyTooLong
	}
	return s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		c, err := s.complaints.GetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if c.UserID != scope.UserID {
			return ErrComplaintNotFound
		}
		if c.Status != ComplaintOpen {
			return ErrComplaintClosed
		}
		uid := scope.UserID
		return s.complaints.AddReplyTx(ctx, tx, &ComplaintReply{
			ID:           s.ids.New(),
			ComplaintID:  id,
			OrgID:        scope.OrgID,
			AuthorUserID: &uid,
			Body:         body,
			CreatedAt:    s.now(),
		})
	})
}

// -----------------------------------------------------------------------------
// Admin
// -----------------------------------------------------------------------------

// AdminListComplaints lists complaints across tenants.
func (s *CommsService) AdminListComplaints(ctx context.Context, query, status string, limit, offset int) ([]AdminComplaintRow, int, error) {
	var out []AdminComplaintRow
	var total int
	err := s.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		rows, n, err := s.adminComplaints.AdminListTx(ctx, tx, query, status, limit, offset)
		if err != nil {
			return err
		}
		out, total = rows, n
		return nil
	})
	return out, total, err
}

// AdminThread is a complaint plus thread for the admin panel.
type AdminThread struct {
	Row     AdminComplaintRow
	Replies []ComplaintReplyView
}

// AdminGetComplaintThread loads any complaint plus its thread.
func (s *CommsService) AdminGetComplaintThread(ctx context.Context, id uuid.UUID) (*AdminThread, error) {
	var thread *AdminThread
	err := s.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := s.adminComplaints.AdminGetTx(ctx, tx, id)
		if err != nil {
			return err
		}
		replies, err := s.adminComplaints.ListRepliesTx(ctx, tx, id)
		if err != nil {
			return err
		}
		thread = &AdminThread{Row: *row, Replies: replies}
		return nil
	})
	return thread, err
}

// AdminReplyToComplaint posts an admin reply and notifies the user.
func (s *CommsService) AdminReplyToComplaint(ctx context.Context, adminID, complaintID uuid.UUID, body, ip string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return ErrCommsSubjectRequired
	}
	if len(body) > 4000 {
		return ErrCommsBodyTooLong
	}
	now := s.now()
	err := s.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := s.adminComplaints.AdminGetTx(ctx, tx, complaintID)
		if err != nil {
			return err
		}
		if err := s.adminComplaints.AddReplyTx(ctx, tx, &ComplaintReply{
			ID:            s.ids.New(),
			ComplaintID:   complaintID,
			OrgID:         row.Complaint.OrgID,
			AuthorAdminID: &adminID,
			Body:          body,
			CreatedAt:     now,
		}); err != nil {
			return err
		}
		return s.adminNotifs.CreateTx(ctx, tx, &Notification{
			ID:        s.ids.New(),
			OrgID:     row.Complaint.OrgID,
			UserID:    row.Complaint.UserID,
			Kind:      NotifKindComplaintReply,
			Title:     "Support replied: " + row.Complaint.Subject,
			Body:      truncate(body, 280),
			Link:      "/support/" + complaintID.String(),
			CreatedAt: now,
		})
	})
	if err != nil {
		return err
	}
	if s.adminAudit != nil {
		_ = s.adminAudit.Record(ctx, adminID, "complaint.reply", "COMPLAINT", complaintID, nil, ip)
	}
	return nil
}

// AdminSetComplaintStatus resolves or reopens a complaint.
func (s *CommsService) AdminSetComplaintStatus(ctx context.Context, adminID, complaintID uuid.UUID, resolved bool, ip string) error {
	now := s.now()
	status := ComplaintOpen
	action := "complaint.reopen"
	if resolved {
		status = ComplaintResolved
		action = "complaint.resolve"
	}
	var notify *Notification
	err := s.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := s.adminComplaints.AdminGetTx(ctx, tx, complaintID)
		if err != nil {
			return err
		}
		if err := s.adminComplaints.SetStatusTx(ctx, tx, complaintID, status, now); err != nil {
			return err
		}
		if resolved {
			notify = &Notification{
				ID:        s.ids.New(),
				OrgID:     row.Complaint.OrgID,
				UserID:    row.Complaint.UserID,
				Kind:      NotifKindComplaintResolve,
				Title:     "Resolved: " + row.Complaint.Subject,
				Body:      "Your support request was marked as resolved. Reply here if the issue persists.",
				Link:      "/support/" + complaintID.String(),
				CreatedAt: now,
			}
			return s.adminNotifs.CreateTx(ctx, tx, notify)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.adminAudit != nil {
		_ = s.adminAudit.Record(ctx, adminID, action, "COMPLAINT", complaintID, nil, ip)
	}
	return nil
}

// AdminBroadcast fans a message out to every user with an active membership.
func (s *CommsService) AdminBroadcast(ctx context.Context, adminID uuid.UUID, title, body, link, ip string) (int64, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	link = strings.TrimSpace(link)
	if title == "" {
		return 0, ErrCommsTitleRequired
	}
	if len(title) > 160 {
		return 0, ErrCommsTitleTooLong
	}
	if len(body) > 2000 {
		return 0, ErrCommsBodyTooLong
	}
	batchID := s.ids.New()
	var recipients int64
	err := s.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		n, err := s.adminNotifs.BroadcastTx(ctx, tx, batchID, title, body, link)
		if err != nil {
			return err
		}
		recipients = n
		return nil
	})
	if err != nil {
		return 0, err
	}
	if s.adminAudit != nil {
		_ = s.adminAudit.Record(ctx, adminID, "broadcast.sent", "BROADCAST", batchID,
			map[string]any{"recipients": recipients, "title": title}, ip)
	}
	return recipients, nil
}

// AdminListBroadcasts lists sent broadcast batches.
func (s *CommsService) AdminListBroadcasts(ctx context.Context, query string, limit, offset int) ([]BroadcastSummary, int, error) {
	var out []BroadcastSummary
	var total int
	err := s.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		rows, n, err := s.adminNotifs.ListBroadcastsTx(ctx, tx, query, limit, offset)
		if err != nil {
			return err
		}
		out, total = rows, n
		return nil
	})
	return out, total, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
