package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// NotificationRepo persists the in-app feed. Construct it with the tenant
// DB for user paths or the admin DB (BYPASSRLS) for cross-tenant admin paths.
type NotificationRepo struct {
	db *DB
}

func NewNotificationRepo(db *DB) *NotificationRepo { return &NotificationRepo{db: db} }

// CreateTx inserts one feed row.
func (r *NotificationRepo) CreateTx(ctx context.Context, tx pgx.Tx, n *app.Notification) error {
	const q = `
		INSERT INTO notifications
			(id, organization_id, user_id, kind, title, body, link, batch_id, is_read, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`
	_, err := tx.Exec(ctx, q,
		n.ID, n.OrgID, n.UserID, n.Kind, n.Title, n.Body, n.Link, n.BatchID, n.IsRead, n.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("notification_repo: create: %w", Classify(err))
	}
	return nil
}

// BroadcastTx fans one message out to every user with an active membership,
// each member under their oldest active org. Returns the recipient count.
func (r *NotificationRepo) BroadcastTx(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, title, body, link string) (int64, error) {
	const q = `
		INSERT INTO notifications
			(id, organization_id, user_id, kind, title, body, link, batch_id)
		SELECT gen_random_uuid(), m.organization_id, u.id, 'broadcast', $1, $2, $3, $4
		FROM users u
		JOIN LATERAL (
			SELECT organization_id
			FROM organization_members
			WHERE user_id = u.id AND status = 'ACTIVE'
			ORDER BY created_at ASC
			LIMIT 1
		) m ON true
	`
	ct, err := tx.Exec(ctx, q, title, body, nullIfEmpty(link), batchID)
	if err != nil {
		return 0, fmt.Errorf("notification_repo: broadcast: %w", Classify(err))
	}
	return ct.RowsAffected(), nil
}

// ListForUserTx returns the user's feed page plus the total count.
func (r *NotificationRepo) ListForUserTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, limit, offset int) ([]app.Notification, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1`, userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("notification_repo: count: %w", Classify(err))
	}
	const q = `
		SELECT id, organization_id, user_id, kind, title, body, link, batch_id,
		       is_read, read_at, created_at
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := tx.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("notification_repo: list: %w", Classify(err))
	}
	defer rows.Close()
	var out []app.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, total, rows.Err()
}

// CountUnreadTx returns the number of unread feed rows.
func (r *NotificationRepo) CountUnreadTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (int, error) {
	var n int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE user_id = $1 AND is_read = FALSE`, userID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("notification_repo: unread: %w", Classify(err))
	}
	return n, nil
}

// MarkReadTx marks one row read. Idempotent: unknown or already-read ids
// are silently ignored.
func (r *NotificationRepo) MarkReadTx(ctx context.Context, tx pgx.Tx, userID, notifID uuid.UUID, now time.Time) error {
	const q = `
		UPDATE notifications
		SET is_read = TRUE, read_at = $3
		WHERE id = $1 AND user_id = $2 AND is_read = FALSE
	`
	if _, err := tx.Exec(ctx, q, notifID, userID, now); err != nil {
		return fmt.Errorf("notification_repo: mark read: %w", Classify(err))
	}
	return nil
}

// MarkAllReadTx marks the whole feed read.
func (r *NotificationRepo) MarkAllReadTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) error {
	const q = `
		UPDATE notifications
		SET is_read = TRUE, read_at = $2
		WHERE user_id = $1 AND is_read = FALSE
	`
	if _, err := tx.Exec(ctx, q, userID, now); err != nil {
		return fmt.Errorf("notification_repo: mark all read: %w", Classify(err))
	}
	return nil
}

// ListBroadcastsTx lists sent broadcast batches, newest first.
func (r *NotificationRepo) ListBroadcastsTx(ctx context.Context, tx pgx.Tx, query string, limit, offset int) ([]app.BroadcastSummary, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	where := `batch_id IS NOT NULL`
	var args []any
	if q := strings.TrimSpace(query); q != "" {
		args = append(args, "%"+q+"%")
		where += fmt.Sprintf(` AND (title ILIKE $%d OR body ILIKE $%d)`, len(args), len(args))
	}

	var total int
	countSQL := fmt.Sprintf(`SELECT count(DISTINCT batch_id) FROM notifications WHERE %s`, where)
	if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("notification_repo: broadcasts count: %w", Classify(err))
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, limit, offset)
	listSQL := fmt.Sprintf(`
		SELECT batch_id, MIN(title), MIN(body), MIN(link), count(*), MIN(created_at)
		FROM notifications
		WHERE %s
		GROUP BY batch_id
		ORDER BY MIN(created_at) DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2)
	rows, err := tx.Query(ctx, listSQL, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("notification_repo: broadcasts list: %w", Classify(err))
	}
	defer rows.Close()
	var out []app.BroadcastSummary
	for rows.Next() {
		var b app.BroadcastSummary
		var link *string
		if err := rows.Scan(&b.BatchID, &b.Title, &b.Body, &link, &b.Recipients, &b.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("notification_repo: broadcasts scan: %w", err)
		}
		if link != nil {
			b.Link = *link
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func scanNotification(row scannable) (app.Notification, error) {
	var n app.Notification
	var readAt *time.Time
	if err := row.Scan(&n.ID, &n.OrgID, &n.UserID, &n.Kind, &n.Title, &n.Body,
		&n.Link, &n.BatchID, &n.IsRead, &readAt, &n.CreatedAt); err != nil {
		return app.Notification{}, fmt.Errorf("notification_repo: scan: %w", err)
	}
	n.ReadAt = readAt
	return n, nil
}

// -----------------------------------------------------------------------------
// Complaints
// -----------------------------------------------------------------------------

// ComplaintRepo persists support complaints and threads. Same dual-DB
// construction as NotificationRepo.
type ComplaintRepo struct {
	db *DB
}

func NewComplaintRepo(db *DB) *ComplaintRepo { return &ComplaintRepo{db: db} }

// CreateTx files a complaint.
func (r *ComplaintRepo) CreateTx(ctx context.Context, tx pgx.Tx, c *app.Complaint) error {
	const q = `
		INSERT INTO complaints
			(id, organization_id, user_id, subject, message, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
	`
	_, err := tx.Exec(ctx, q, c.ID, c.OrgID, c.UserID, c.Subject, c.Message, c.Status, c.CreatedAt)
	if err != nil {
		return fmt.Errorf("complaint_repo: create: %w", Classify(err))
	}
	return nil
}

// ListForUserTx returns the user's complaints plus the total count.
func (r *ComplaintRepo) ListForUserTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, limit, offset int) ([]app.Complaint, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM complaints WHERE user_id = $1`, userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("complaint_repo: count: %w", Classify(err))
	}
	const q = `
		SELECT id, organization_id, user_id, subject, message, status,
		       created_at, updated_at, resolved_at
		FROM complaints
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := tx.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("complaint_repo: list: %w", Classify(err))
	}
	defer rows.Close()
	var out []app.Complaint
	for rows.Next() {
		c, err := scanComplaint(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// GetTx loads one complaint.
func (r *ComplaintRepo) GetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*app.Complaint, error) {
	const q = `
		SELECT id, organization_id, user_id, subject, message, status,
		       created_at, updated_at, resolved_at
		FROM complaints
		WHERE id = $1
	`
	c, err := scanComplaint(tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AddReplyTx appends a thread reply and bumps the complaint's updated_at.
func (r *ComplaintRepo) AddReplyTx(ctx context.Context, tx pgx.Tx, reply *app.ComplaintReply) error {
	const q = `
		INSERT INTO complaint_replies
			(id, complaint_id, organization_id, author_user_id, author_admin_id, body, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`
	if _, err := tx.Exec(ctx, q, reply.ID, reply.ComplaintID, reply.OrgID,
		reply.AuthorUserID, reply.AuthorAdminID, reply.Body, reply.CreatedAt); err != nil {
		return fmt.Errorf("complaint_repo: reply: %w", Classify(err))
	}
	if _, err := tx.Exec(ctx,
		`UPDATE complaints SET updated_at = $2 WHERE id = $1`, reply.ComplaintID, reply.CreatedAt); err != nil {
		return fmt.Errorf("complaint_repo: touch: %w", Classify(err))
	}
	return nil
}

// ListRepliesTx returns the thread oldest-first with display authors.
func (r *ComplaintRepo) ListRepliesTx(ctx context.Context, tx pgx.Tx, complaintID uuid.UUID) ([]app.ComplaintReplyView, error) {
	const q = `
		SELECT r.id, r.complaint_id, r.organization_id, r.author_user_id, r.author_admin_id,
		       r.body, r.created_at,
		       COALESCE(u.name, 'Ordora Support')
		FROM complaint_replies r
		LEFT JOIN users u ON u.id = r.author_user_id
		WHERE r.complaint_id = $1
		ORDER BY r.created_at ASC
	`
	rows, err := tx.Query(ctx, q, complaintID)
	if err != nil {
		return nil, fmt.Errorf("complaint_repo: replies: %w", Classify(err))
	}
	defer rows.Close()
	var out []app.ComplaintReplyView
	for rows.Next() {
		var v app.ComplaintReplyView
		if err := rows.Scan(&v.Reply.ID, &v.Reply.ComplaintID, &v.Reply.OrgID,
			&v.Reply.AuthorUserID, &v.Reply.AuthorAdminID, &v.Reply.Body, &v.Reply.CreatedAt,
			&v.AuthorName); err != nil {
			return nil, fmt.Errorf("complaint_repo: replies scan: %w", err)
		}
		v.IsAdmin = v.Reply.AuthorAdminID != nil
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetStatusTx resolves or reopens a complaint.
func (r *ComplaintRepo) SetStatusTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, now time.Time) error {
	const q = `
		UPDATE complaints
		SET status = $2, updated_at = $3,
		    resolved_at = CASE WHEN $2 = 'RESOLVED' THEN $3 ELSE NULL END
		WHERE id = $1
	`
	ct, err := tx.Exec(ctx, q, id, status, now)
	if err != nil {
		return fmt.Errorf("complaint_repo: status: %w", Classify(err))
	}
	if ct.RowsAffected() == 0 {
		return app.ErrComplaintNotFound
	}
	return nil
}

// AdminListTx lists complaints across tenants with user/org context.
func (r *ComplaintRepo) AdminListTx(ctx context.Context, tx pgx.Tx, query, status string, limit, offset int) ([]app.AdminComplaintRow, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	clauses := []string{"TRUE"}
	var args []any
	if status == app.ComplaintOpen || status == app.ComplaintResolved {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf(`c.status = $%d`, len(args)))
	}
	if q := strings.TrimSpace(query); q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf(
			`(c.subject ILIKE $%d OR c.message ILIKE $%d OR u.email::text ILIKE $%d OR o.name ILIKE $%d)`,
			n, n, n, n,
		))
	}
	where := strings.Join(clauses, " AND ")

	var total int
	countSQL := fmt.Sprintf(`
		SELECT count(*)
		FROM complaints c
		JOIN users u ON u.id = c.user_id
		JOIN organizations o ON o.id = c.organization_id
		WHERE %s`, where)
	if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("complaint_repo: admin count: %w", Classify(err))
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, limit, offset)
	listSQL := fmt.Sprintf(`
		SELECT c.id, c.organization_id, c.user_id, c.subject, c.message, c.status,
		       c.created_at, c.updated_at, c.resolved_at,
		       u.name, u.email::text, o.name,
		       (SELECT count(*) FROM complaint_replies r WHERE r.complaint_id = c.id),
		       (SELECT MAX(r.created_at) FROM complaint_replies r WHERE r.complaint_id = c.id)
		FROM complaints c
		JOIN users u ON u.id = c.user_id
		JOIN organizations o ON o.id = c.organization_id
		WHERE %s
		ORDER BY c.updated_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2)
	rows, err := tx.Query(ctx, listSQL, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("complaint_repo: admin list: %w", Classify(err))
	}
	defer rows.Close()
	var out []app.AdminComplaintRow
	for rows.Next() {
		row, err := scanAdminComplaintRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// AdminGetTx loads one complaint with user/org context.
func (r *ComplaintRepo) AdminGetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*app.AdminComplaintRow, error) {
	const q = `
		SELECT c.id, c.organization_id, c.user_id, c.subject, c.message, c.status,
		       c.created_at, c.updated_at, c.resolved_at,
		       u.name, u.email::text, o.name,
		       (SELECT count(*) FROM complaint_replies r WHERE r.complaint_id = c.id),
		       (SELECT MAX(r.created_at) FROM complaint_replies r WHERE r.complaint_id = c.id)
		FROM complaints c
		JOIN users u ON u.id = c.user_id
		JOIN organizations o ON o.id = c.organization_id
		WHERE c.id = $1
	`
	row, err := scanAdminComplaintRow(tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func scanComplaint(row scannable) (app.Complaint, error) {
	var c app.Complaint
	if err := row.Scan(&c.ID, &c.OrgID, &c.UserID, &c.Subject, &c.Message, &c.Status,
		&c.CreatedAt, &c.UpdatedAt, &c.ResolvedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.Complaint{}, app.ErrComplaintNotFound
		}
		return app.Complaint{}, fmt.Errorf("complaint_repo: scan: %w", err)
	}
	return c, nil
}

func scanAdminComplaintRow(row scannable) (app.AdminComplaintRow, error) {
	var r app.AdminComplaintRow
	if err := row.Scan(&r.Complaint.ID, &r.Complaint.OrgID, &r.Complaint.UserID,
		&r.Complaint.Subject, &r.Complaint.Message, &r.Complaint.Status,
		&r.Complaint.CreatedAt, &r.Complaint.UpdatedAt, &r.Complaint.ResolvedAt,
		&r.UserName, &r.UserEmail, &r.OrgName, &r.ReplyCount, &r.LastReplyAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.AdminComplaintRow{}, app.ErrComplaintNotFound
		}
		return app.AdminComplaintRow{}, fmt.Errorf("complaint_repo: scan admin: %w", err)
	}
	return r, nil
}
