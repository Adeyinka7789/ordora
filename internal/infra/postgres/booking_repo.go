package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/booking"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// BookingRepo persists booking_settings/hours/blackouts/bookings. Tenant
// methods take a scope and open their own WithTenant block (repo
// convention); anonymous reads go through SECURITY DEFINER functions.
type BookingRepo struct {
	db  *DB
	ids app.IDGen
}

func NewBookingRepo(db *DB, ids app.IDGen) *BookingRepo { return &BookingRepo{db: db, ids: ids} }

// GetSettings returns the org's booking settings, or defaults when the
// owner never saved any.
func (r *BookingRepo) GetSettings(ctx context.Context, scope tenant.TenantScope) (*app.BookingSettings, error) {
	var out *app.BookingSettings
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT organization_id, accepting_bookings, default_duration_min,
			       buffer_before_min, buffer_after_min, min_notice_hours,
			       max_advance_days, COALESCE(cancellation_policy,'')
			FROM booking_settings
			WHERE organization_id = $1
		`
		var s app.BookingSettings
		if err := tx.QueryRow(ctx, q, scope.OrgID).Scan(
			&s.OrgID, &s.Accepting, &s.DurationMin, &s.BufBefore, &s.BufAfter,
			&s.MinNoticeHours, &s.MaxAdvanceDays, &s.CancelPolicy,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				s = app.DefaultBookingSettings(scope.OrgID)
				out = &s
				return nil
			}
			return fmt.Errorf("booking_repo: get settings: %w", Classify(err))
		}
		out = &s
		return nil
	})
	return out, err
}

// UpsertSettings saves the org's booking settings (owner-only upstream).
func (r *BookingRepo) UpsertSettings(ctx context.Context, scope tenant.TenantScope, s *app.BookingSettings) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO booking_settings
			    (organization_id, accepting_bookings, default_duration_min,
			     buffer_before_min, buffer_after_min, min_notice_hours,
			     max_advance_days, cancellation_policy, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (organization_id)
			DO UPDATE SET accepting_bookings = EXCLUDED.accepting_bookings,
			              default_duration_min = EXCLUDED.default_duration_min,
			              buffer_before_min = EXCLUDED.buffer_before_min,
			              buffer_after_min = EXCLUDED.buffer_after_min,
			              min_notice_hours = EXCLUDED.min_notice_hours,
			              max_advance_days = EXCLUDED.max_advance_days,
			              cancellation_policy = EXCLUDED.cancellation_policy,
			              updated_at = EXCLUDED.updated_at
		`
		_, err := tx.Exec(ctx, q, scope.OrgID,
			s.Accepting, s.DurationMin, s.BufBefore, s.BufAfter,
			s.MinNoticeHours, s.MaxAdvanceDays, s.CancelPolicy, s.UpdatedAt)
		if err != nil {
			return fmt.Errorf("booking_repo: upsert settings: %w", Classify(err))
		}
		return nil
	})
}

// SetHours replaces the weekly schedule (7 rows max, owner-only upstream).
func (r *BookingRepo) SetHours(ctx context.Context, scope tenant.TenantScope, hours []app.BookingHour) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM booking_hours WHERE organization_id = $1`, scope.OrgID); err != nil {
			return fmt.Errorf("booking_repo: clear hours: %w", Classify(err))
		}
		for _, h := range hours {
			if _, err := tx.Exec(ctx, `
				INSERT INTO booking_hours (organization_id, dow, open_min, close_min, is_open)
				VALUES ($1, $2, $3, $4, $5)
			`, scope.OrgID, h.Dow, h.OpenMin, h.CloseMin, h.IsOpen); err != nil {
				return fmt.Errorf("booking_repo: set hours: %w", Classify(err))
			}
		}
		return nil
	})
}

// ListHours returns stored weekly rows (empty = code defaults apply).
func (r *BookingRepo) ListHours(ctx context.Context, scope tenant.TenantScope) ([]app.BookingHour, error) {
	var out []app.BookingHour
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT dow, open_min, close_min, is_open
			FROM booking_hours WHERE organization_id = $1 ORDER BY dow
		`, scope.OrgID)
		if err != nil {
			return fmt.Errorf("booking_repo: list hours: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var h app.BookingHour
			if err := rows.Scan(&h.Dow, &h.OpenMin, &h.CloseMin, &h.IsOpen); err != nil {
				return fmt.Errorf("booking_repo: scan hours: %w", err)
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// AddBlackout closes one date (idempotent).
func (r *BookingRepo) AddBlackout(ctx context.Context, scope tenant.TenantScope, day time.Time, reason string, id uuid.UUID, now time.Time) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO booking_blackouts (id, organization_id, day, reason, created_at)
			VALUES ($1, $2, $3::date, $4, $5)
			ON CONFLICT (organization_id, day) DO UPDATE SET reason = EXCLUDED.reason
		`
		_, err := tx.Exec(ctx, q, id, scope.OrgID, day.Format("2006-01-02"), reason, now)
		if err != nil {
			return fmt.Errorf("booking_repo: add blackout: %w", Classify(err))
		}
		return nil
	})
}

// RemoveBlackout reopens one date.
func (r *BookingRepo) RemoveBlackout(ctx context.Context, scope tenant.TenantScope, day time.Time) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM booking_blackouts WHERE organization_id = $1 AND day = $2::date
		`, scope.OrgID, day.Format("2006-01-02"))
		if err != nil {
			return fmt.Errorf("booking_repo: remove blackout: %w", Classify(err))
		}
		return nil
	})
}

// ListBlackouts returns blackouts in [from, to].
func (r *BookingRepo) ListBlackouts(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]app.BookingBlackout, error) {
	var out []app.BookingBlackout
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, day, reason FROM booking_blackouts
			WHERE organization_id = $1 AND day BETWEEN $2::date AND $3::date
			ORDER BY day
		`, scope.OrgID, from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err != nil {
			return fmt.Errorf("booking_repo: list blackouts: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var b app.BookingBlackout
			if err := rows.Scan(&b.ID, &b.Day, &b.Reason); err != nil {
				return fmt.Errorf("booking_repo: scan blackout: %w", err)
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// LockOrgTx serializes booking writes for one org (overlap check safety).
func (r *BookingRepo) LockOrgTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	const q = `SELECT id FROM organizations WHERE id = $1 FOR UPDATE`
	var id uuid.UUID
	if err := tx.QueryRow(ctx, q, orgID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("booking_repo: lock org: no such organization")
		}
		return fmt.Errorf("booking_repo: lock org: %w", Classify(err))
	}
	return nil
}

// scanBookingRow materializes a bookings row joined with order/customer.
func scanBookingRow(row pgx.Row) (*app.Booking, error) {
	var b app.Booking
	var status string
	if err := row.Scan(&b.ID, &b.OrgID, &b.OrderID, &b.CustomerID, &b.ProductID,
		&b.StaffUserID, &b.StartAt, &b.EndAt, &b.Timezone,
		&b.BufBefore, &b.BufAfter, &status,
		&b.CustomerNotes, &b.CancelReason, &b.RescheduledAt, &b.PrevStartAt,
		&b.CreatedAt, &b.UpdatedAt,
		&b.OrderNumber, &b.OrderStatus, &b.CustomerName, &b.CustomerPhone,
		&b.CustomerEmail, &b.ServiceName, &b.TotalMinor, &b.PaidMinor, &b.Currency,
		&b.StaffName,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.ErrBookingNotFound
		}
		return nil, fmt.Errorf("booking_repo: scan: %w", err)
	}
	b.Status = booking.Status(status)
	return &b, nil
}

const bookingJoins = `
	FROM bookings b
	JOIN orders o ON o.id = b.order_id
	JOIN customers c ON c.id = b.customer_id
	LEFT JOIN products p ON p.id = b.product_id
	LEFT JOIN users u ON u.id = b.staff_user_id
	WHERE b.organization_id = $1
`

const bookingColumns = `
	SELECT b.id, b.organization_id, b.order_id, b.customer_id, b.product_id,
	       b.staff_user_id, b.start_at, b.end_at, b.timezone,
	       b.buf_before_min, b.buf_after_min, b.status,
	       b.customer_notes, b.cancel_reason, b.rescheduled_at, b.prev_start_at,
	       b.created_at, b.updated_at,
	       o.order_number, o.status::text, c.name, COALESCE(c.phone,''),
	       COALESCE(c.email::text,''), COALESCE(p.name,''),
	       o.total_minor, o.amount_paid_minor, o.currency::text,
	       COALESCE(u.name,'')
`

// GetBooking loads one booking with order/customer context.
func (r *BookingRepo) GetBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*app.Booking, error) {
	var out *app.Booking
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = bookingColumns + bookingJoins + ` AND b.id = $2`
		b, err := scanBookingRow(tx.QueryRow(ctx, q, scope.OrgID, id))
		if err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}

// ListDayBookings lists bookings overlapping a UTC window (panel day view).
func (r *BookingRepo) ListDayBookings(ctx context.Context, scope tenant.TenantScope, fromUTC, toUTC time.Time) ([]app.Booking, error) {
	var out []app.Booking
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = bookingColumns + bookingJoins + `
			AND tstzrange(b.start_at, b.end_at, '[)') && tstzrange($2, $3, '[)')
			ORDER BY b.start_at ASC`
		rows, err := tx.Query(ctx, q, scope.OrgID, fromUTC, toUTC)
		if err != nil {
			return fmt.Errorf("booking_repo: list day: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			b, err := scanBookingRow(rows)
			if err != nil {
				return err
			}
			out = append(out, *b)
		}
		return rows.Err()
	})
	return out, err
}

// UpdateBookingStatus sets status (conditional on expected-from) plus an
// optional cancel reason. Zero rows = stale/missing (caller maps).
func (r *BookingRepo) UpdateBookingStatus(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, from, to booking.Status, cancelReason string, now time.Time) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if err := r.LockOrgTx(ctx, tx, scope.OrgID); err != nil {
			return err
		}
		const q = `
			UPDATE bookings
			SET status = $4, cancel_reason = $5, updated_at = $6
			WHERE organization_id = $1 AND id = $2 AND status = $3
		`
		ct, err := tx.Exec(ctx, q, scope.OrgID, id, string(from), string(to), cancelReason, now)
		if err != nil {
			return fmt.Errorf("booking_repo: update status: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return app.ErrBookingStale
		}
		return nil
	})
}

// checkMoveTx enforces the scheduling rules for a new appointment time:
// future, inside notice/advance window, within weekly hours, not on a
// blackout. Reads everything inside the caller's transaction so a
// concurrent settings edit cannot slip a bad move through.
func (r *BookingRepo) checkMoveTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, newStart, newEnd, now time.Time) error {
	var tz string
	if err := tx.QueryRow(ctx,
		`SELECT timezone FROM organizations WHERE id = $1`, orgID).Scan(&tz); err != nil {
		return fmt.Errorf("booking_repo: move org: %w", Classify(err))
	}
	var notice, advance int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(s.min_notice_hours, 2), COALESCE(s.max_advance_days, 30)
		  FROM (SELECT 1) AS one
		  LEFT JOIN booking_settings s ON s.organization_id = $1
	`, orgID).Scan(&notice, &advance); err != nil {
		return fmt.Errorf("booking_repo: move rules: %w", Classify(err))
	}
	if !newStart.After(now) ||
		newStart.Before(now.Add(time.Duration(notice)*time.Hour)) ||
		!newStart.Before(moveWindowDeadline(now, tz, advance)) {
		return app.ErrBookingBadSlot
	}
	var openMin, closeMin *int
	var isOpen *bool
	hoursErr := tx.QueryRow(ctx, `
		SELECT h.open_min, h.close_min, h.is_open FROM booking_hours h
		WHERE h.organization_id = $1 AND h.dow = EXTRACT(DOW FROM $2::timestamptz AT TIME ZONE $3)::integer
	`, orgID, newStart, tz).Scan(&openMin, &closeMin, &isOpen)
	if hoursErr != nil && !errors.Is(hoursErr, pgx.ErrNoRows) {
		return fmt.Errorf("booking_repo: move hours: %w", Classify(hoursErr))
	}
	open, close, ok := 540, 1020, true
	if hoursErr == nil {
		open, close, ok = *openMin, *closeMin, *isOpen
	} else {
		// Default week mirrors the app: Sunday closed.
		ok = newStart.In(app.BookingTimezone(tz)).Weekday() != time.Sunday
	}
	if !ok {
		return app.ErrBookingBadSlot
	}
	var startMin, endMin int
	if err := tx.QueryRow(ctx, `
		SELECT EXTRACT(HOUR FROM $1::timestamptz AT TIME ZONE $2)::integer * 60 +
		       EXTRACT(MINUTE FROM $1::timestamptz AT TIME ZONE $2)::integer,
		       EXTRACT(HOUR FROM $3::timestamptz AT TIME ZONE $2)::integer * 60 +
		       EXTRACT(MINUTE FROM $3::timestamptz AT TIME ZONE $2)::integer
	`, newStart, tz, newEnd).Scan(&startMin, &endMin); err != nil {
		return fmt.Errorf("booking_repo: move clock: %w", Classify(err))
	}
	if startMin < open || endMin > close {
		return app.ErrBookingBadSlot
	}
	var blacked bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM booking_blackouts b
			WHERE b.organization_id = $1
			  AND b.day = ($2::timestamptz AT TIME ZONE $3)::date)
	`, orgID, newStart, tz).Scan(&blacked); err != nil {
		return fmt.Errorf("booking_repo: move blackout: %w", Classify(err))
	}
	if blacked {
		return app.ErrBookingBadSlot
	}
	return nil
}

// moveWindowDeadline is the exclusive end of the bookable window: midnight
// after the last bookable org-local day (same day-granularity rule the
// public submit path enforces).
func moveWindowDeadline(now time.Time, tz string, advanceDays int) time.Time {
	loc := app.BookingTimezone(tz)
	latest := app.LocalMidnight(now.In(loc).AddDate(0, 0, advanceDays), loc)
	return latest.Add(24 * time.Hour)
}

// RescheduleTx moves a booking after re-checking overlap (excluding self).
// Runs inside the caller's lock; the row is re-read FOR UPDATE.
func (r *BookingRepo) RescheduleTx(ctx context.Context, tx pgx.Tx, scope tenant.TenantScope, id uuid.UUID, newStart, newEnd time.Time, now time.Time) (*app.Booking, error) {
	const get = bookingColumns + `
		FROM bookings b
		JOIN orders o ON o.id = b.order_id
		JOIN customers c ON c.id = b.customer_id
		LEFT JOIN products p ON p.id = b.product_id
		LEFT JOIN users u ON u.id = b.staff_user_id
		WHERE b.organization_id = $1 AND b.id = $2
		FOR UPDATE OF b
	`
	b, err := scanBookingRow(tx.QueryRow(ctx, get, scope.OrgID, id))
	if err != nil {
		return nil, err
	}
	if b.Status != booking.StatusRequested && b.Status != booking.StatusConfirmed {
		return nil, app.ErrBookingBadStatus
	}
	// A move keeps its duration: the staff form only shifts the start.
	if !newEnd.Equal(newStart.Add(b.EndAt.Sub(b.StartAt))) {
		return nil, app.ErrBookingBadSlot
	}
	// Authoritative re-validation inside the lock. The staff UI pre-checks
	// all of this, but hours, blackouts, and scheduling rules can change
	// between render and submit — the transaction holds the line.
	if err := r.checkMoveTx(ctx, tx, scope.OrgID, newStart, newEnd, now); err != nil {
		return nil, err
	}
	const overlap = `
		SELECT 1 FROM bookings o
		WHERE o.organization_id = $1 AND o.id <> $2
		  AND o.status IN ('REQUESTED','CONFIRMED')
		  AND ($5::uuid IS NULL OR o.staff_user_id IS NULL OR o.staff_user_id = $5)
		  AND o.start_at - (o.buf_before_min || ' minutes')::interval < $4
		  AND o.end_at + (o.buf_after_min || ' minutes')::interval > $3
		LIMIT 1
	`
	var one int
	if err := tx.QueryRow(ctx, overlap, scope.OrgID, id,
		newStart.Add(-time.Duration(b.BufBefore)*time.Minute),
		newEnd.Add(time.Duration(b.BufAfter)*time.Minute),
		b.StaffUserID,
	).Scan(&one); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("booking_repo: reschedule overlap: %w", Classify(err))
	}
	if one == 1 {
		return nil, app.ErrBookingSlotTaken
	}
	const upd = `
		UPDATE bookings
		SET start_at = $3, end_at = $4, rescheduled_at = $5,
		    prev_start_at = start_at, updated_at = $5
		WHERE organization_id = $1 AND id = $2
		RETURNING start_at, end_at
	`
	if err := tx.QueryRow(ctx, upd, scope.OrgID, id, newStart, newEnd, now).Scan(&b.StartAt, &b.EndAt); err != nil {
		return nil, fmt.Errorf("booking_repo: reschedule update: %w", Classify(err))
	}
	b.RescheduledAt = &now
	// Keep the linked order's date in step (date part only; the booking
	// row stays authoritative for time).
	if _, err := tx.Exec(ctx, `
		UPDATE orders SET expected_completion = ($3 AT TIME ZONE $4)::date, updated_at = $5
		WHERE organization_id = $1 AND id = $2
	`, scope.OrgID, b.OrderID, newStart, b.Timezone, now); err != nil {
		return nil, fmt.Errorf("booking_repo: reschedule order date: %w", Classify(err))
	}
	return b, nil
}

// Reschedule moves a booking under the org lock.
func (r *BookingRepo) Reschedule(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, newStart, newEnd time.Time, now time.Time) (*app.Booking, error) {
	var out *app.Booking
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if err := r.LockOrgTx(ctx, tx, scope.OrgID); err != nil {
			return err
		}
		b, err := r.RescheduleTx(ctx, tx, scope, id, newStart, newEnd, now)
		if err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}

// BookingConfig loads the anonymous booking configuration for one slug.
// Unknown slugs read as ineligible (callers 404 via LookupOrg first).
func (r *BookingRepo) BookingConfig(ctx context.Context, slug string) (*app.BookingConfig, error) {
	var out *app.BookingConfig
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT org_id, eligible, flag_on, accepting, duration_min,
			       buf_before, buf_after, min_notice_hours, max_advance_days,
			       cancel_policy, org_timezone, currency, org_name
			FROM get_booking_config($1)
		`
		var c app.BookingConfig
		var orgID *uuid.UUID
		if err := tx.QueryRow(ctx, q, slug).Scan(
			&orgID, &c.Eligible, &c.FlagOn, &c.Accepting, &c.DurationMin,
			&c.BufBefore, &c.BufAfter, &c.MinNoticeHours, &c.MaxAdvanceDays,
			&c.CancelPolicy, &c.Timezone, &c.Currency, &c.OrgName,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				out = &app.BookingConfig{}
				return nil
			}
			return fmt.Errorf("booking_repo: config: %w", Classify(err))
		}
		if orgID != nil {
			c.OrgID = *orgID
		}
		out = &c
		return nil
	})
	return out, err
}

// BookingHours loads stored weekly rows for a slug (empty = defaults).
func (r *BookingRepo) BookingHours(ctx context.Context, slug string) ([]app.BookingHour, error) {
	var out []app.BookingHour
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT dow, open_min, close_min, is_open FROM get_booking_hours($1)`, slug)
		if err != nil {
			return fmt.Errorf("booking_repo: hours: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var h app.BookingHour
			if err := rows.Scan(&h.Dow, &h.OpenMin, &h.CloseMin, &h.IsOpen); err != nil {
				return fmt.Errorf("booking_repo: scan hours: %w", err)
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// BookingBlackouts loads blackouts in [from, to] for a slug.
func (r *BookingRepo) BookingBlackouts(ctx context.Context, slug string, from, to time.Time) ([]app.BookingBlackout, error) {
	var out []app.BookingBlackout
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT day, reason FROM get_booking_blackouts($1, $2::date, $3::date)`,
			slug, from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err != nil {
			return fmt.Errorf("booking_repo: blackouts: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var b app.BookingBlackout
			if err := rows.Scan(&b.Day, &b.Reason); err != nil {
				return fmt.Errorf("booking_repo: scan blackout: %w", err)
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// BookingBusy loads live appointments overlapping a UTC window.
func (r *BookingRepo) BookingBusy(ctx context.Context, slug string, fromUTC, toUTC time.Time) ([]app.BusyBlock, error) {
	var out []app.BusyBlock
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT start_at, end_at, buf_before, buf_after, staff_id FROM get_booking_busy($1, $2, $3)`,
			slug, fromUTC, toUTC)
		if err != nil {
			return fmt.Errorf("booking_repo: busy: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var b app.BusyBlock
			if err := rows.Scan(&b.Start, &b.End, &b.BufBefore, &b.BufAfter, &b.StaffUserID); err != nil {
				return fmt.Errorf("booking_repo: scan busy: %w", err)
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// BookingStaff lists bookable team members (active writers, owner first).
func (r *BookingRepo) BookingStaff(ctx context.Context, slug string) ([]app.BookingStaffMember, error) {
	var out []app.BookingStaffMember
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT user_id, name FROM get_booking_staff($1)`, slug)
		if err != nil {
			return fmt.Errorf("booking_repo: staff: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var m app.BookingStaffMember
			if err := rows.Scan(&m.UserID, &m.Name); err != nil {
				return fmt.Errorf("booking_repo: scan staff: %w", err)
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// CreateBooking runs create_booking: org lock, re-validation, customer +
// order + item + booking + tracking token, atomically. The raw token is
// minted here and returned once for the confirmation page.
func (r *BookingRepo) CreateBooking(ctx context.Context, in app.CreateBookingInput) (*app.CreateBookingResult, error) {
	raw, hash, err := app.GeneratePublicToken()
	if err != nil {
		return nil, err
	}
	var out *app.CreateBookingResult
	err = r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT o_error, o_order_id, o_order_num, o_total, o_currency, o_org_email, o_booking_id
			FROM create_booking(
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15, $16, $17, $18
			)
		`
		var (
			code      string
			orderID   *uuid.UUID
			bookingID *uuid.UUID
			num       *string
			total     *int64
			curr      *string
			orgMail   *string
		)
		var productID *uuid.UUID
		if in.ProductID != nil {
			productID = in.ProductID
		}
		if err := tx.QueryRow(ctx, q,
			in.Slug, in.CustomerName, in.CustomerEmail,
			in.CustomerPhone, in.Notes, in.ServiceName,
			productID, in.Start, in.End, in.Timezone,
			in.BufBefore, in.BufAfter, hash,
			r.ids.New(), r.ids.New(), r.ids.New(), r.ids.New(),
			in.StaffUserID,
		).Scan(&code, &orderID, &num, &total, &curr, &orgMail, &bookingID); err != nil {
			return fmt.Errorf("booking_repo: create: %w", Classify(err))
		}
		switch code {
		case "":
			// ok
		case "NOT_ELIGIBLE":
			return app.ErrBookingNotEligible
		case "NOT_ACCEPTING":
			return app.ErrBookingClosed
		case "SLOT_TAKEN":
			return app.ErrBookingSlotTaken
		default:
			return app.ErrBookingBadSlot
		}
		res := &app.CreateBookingResult{
			RawToken: raw,
			Start:    in.Start,
			End:      in.End,
		}
		if orderID != nil {
			res.OrderID = *orderID
		}
		if bookingID != nil {
			res.BookingID = *bookingID
		}
		if num != nil {
			res.OrderNumber = *num
		}
		if total != nil {
			res.TotalMinor = *total
		}
		if curr != nil {
			res.Currency = *curr
		}
		if orgMail != nil {
			res.OrgEmail = *orgMail
		}
		out = res
		return nil
	})
	return out, err
}

// BookingByToken resolves an appointment through an order tracking token.
// No rows (nil, nil) on wrong/revoked tokens — no existence signal.
func (r *BookingRepo) BookingByToken(ctx context.Context, hash []byte) (*app.PublicBooking, error) {
	var out *app.PublicBooking
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT start_at, end_at, timezone, status, service_name, org_name, product_id
			FROM get_booking_by_token($1)
		`
		var b app.PublicBooking
		if err := tx.QueryRow(ctx, q, hash).Scan(
			&b.StartAt, &b.EndAt, &b.Timezone, &b.Status, &b.ServiceName, &b.OrgName,
			&b.ProductID,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("booking_repo: by token: %w", Classify(err))
		}
		out = &b
		return nil
	})
	return out, err
}

var _ app.BookingStore = (*BookingRepo)(nil)
