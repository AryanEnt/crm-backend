package documents

import (
	"context"
	"io"
	"path"
	"strings"
	"time"

	"github.com/crm/backend/internal/audit"
	"github.com/crm/backend/internal/automation"
	"github.com/crm/backend/internal/storage"
	"github.com/crm/backend/internal/timeline"
	"github.com/crm/backend/pkg/apperrors"
)

type Service struct {
	repo     *Repository
	store    storage.Store
	audit    *audit.Service
	timeline *timeline.Service
	hooks    *automation.Emitter
}

func NewService(repo *Repository, store storage.Store, auditSvc *audit.Service, timelineSvc *timeline.Service, hooks *automation.Emitter) *Service {
	return &Service{repo: repo, store: store, audit: auditSvc, timeline: timelineSvc, hooks: hooks}
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Document, int, error) {
	_ = s.repo.RefreshExpired(ctx)
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list documents", err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Document, error) {
	_ = s.repo.RefreshExpired(ctx)
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperrors.Internal("failed to load document", err)
	}
	if d == nil {
		return nil, apperrors.NotFound("document not found")
	}
	return d, nil
}

func (s *Service) Request(ctx context.Context, actorID string, in RequestInput, ip, ua string) (*Document, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperrors.Validation("name is required")
	}
	if emptyToNil(in.LeadID) == nil && emptyToNil(in.CustomerID) == nil && emptyToNil(in.DealID) == nil {
		return nil, apperrors.Validation("document must belong to a lead, customer, or deal")
	}
	status := StatusRequested
	if in.Status == StatusMissing {
		status = StatusMissing
	}
	expires, err := parseOptionalDate(in.ExpiresAt)
	if err != nil {
		return nil, apperrors.Validation("invalid expiryDate")
	}
	d, err := s.repo.InsertRequested(ctx, in, expires, status)
	if err != nil {
		return nil, apperrors.Internal("failed to request document", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "document.requested", "document", audit.Ptr(d.ID), map[string]any{
		"name": d.Name, "type": d.DocType, "status": d.Status,
	}, ip, ua)
	s.emit(ctx, actorID, timeline.EventDocumentRequested, "Document requested", d.Name+" ("+d.DocType+")", d)
	return d, nil
}

func (s *Service) UploadNew(ctx context.Context, actorID string, name, docType, category string, leadID, customerID, dealID *string, expiresAt *string, notes string, filename, mime string, size int64, r io.Reader, ip, ua string) (*Document, error) {
	if strings.TrimSpace(name) == "" {
		name = filename
	}
	if strings.TrimSpace(name) == "" {
		return nil, apperrors.Validation("name is required")
	}
	if emptyToNil(leadID) == nil && emptyToNil(customerID) == nil && emptyToNil(dealID) == nil {
		return nil, apperrors.Validation("document must belong to a lead, customer, or deal")
	}
	expires, err := parseOptionalDate(expiresAt)
	if err != nil {
		return nil, apperrors.Validation("invalid expiryDate")
	}
	prefix := "documents"
	if customerID != nil && *customerID != "" {
		prefix = path.Join("documents", "customers", *customerID)
	} else if dealID != nil && *dealID != "" {
		prefix = path.Join("documents", "deals", *dealID)
	} else if leadID != nil && *leadID != "" {
		prefix = path.Join("documents", "leads", *leadID)
	}
	key := storage.NewObjectKey(prefix, filename)
	meta, err := s.store.Put(ctx, key, r, size, mime)
	if err != nil {
		return nil, apperrors.Internal("failed to store file", err)
	}
	d, err := s.repo.CreateUploaded(ctx, name, docType, category, actorID, meta.Key, meta.Provider, mime, meta.Size, leadID, customerID, dealID, expires, notes)
	if err != nil {
		_ = s.store.Delete(ctx, meta.Key)
		return nil, apperrors.Internal("failed to save document", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "document.uploaded", "document", audit.Ptr(d.ID), map[string]any{
		"name": d.Name, "type": d.DocType, "size": d.SizeBytes,
	}, ip, ua)
	s.emit(ctx, actorID, timeline.EventDocumentUploaded, "Document uploaded", d.Name, d)
	if s.hooks != nil {
		_ = s.hooks.Emit(ctx, automation.TriggerDocumentUploaded, "document", d.ID, map[string]any{
			"name": d.Name, "leadId": d.LeadID, "dealId": d.DealID, "customerId": d.CustomerID,
		})
	}
	return d, nil
}

func (s *Service) UploadToExisting(ctx context.Context, actorID, id string, filename, mime string, size int64, r io.Reader, ip, ua string) (*Document, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	prefix := "documents"
	if current.CustomerID != nil {
		prefix = path.Join("documents", "customers", *current.CustomerID)
	} else if current.DealID != nil {
		prefix = path.Join("documents", "deals", *current.DealID)
	} else if current.LeadID != nil {
		prefix = path.Join("documents", "leads", *current.LeadID)
	}
	key := storage.NewObjectKey(prefix, filename)
	meta, err := s.store.Put(ctx, key, r, size, mime)
	if err != nil {
		return nil, apperrors.Internal("failed to store file", err)
	}
	oldKey := current.FileKey
	d, err := s.repo.MarkUploaded(ctx, id, actorID, meta.Key, meta.Provider, mime, meta.Size)
	if err != nil {
		_ = s.store.Delete(ctx, meta.Key)
		return nil, apperrors.Internal("failed to update document", err)
	}
	if oldKey != "" && oldKey != meta.Key {
		_ = s.store.Delete(ctx, oldKey)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "document.uploaded", "document", audit.Ptr(d.ID), map[string]any{
		"name": d.Name, "type": d.DocType,
	}, ip, ua)
	s.emit(ctx, actorID, timeline.EventDocumentUploaded, "Document uploaded", d.Name, d)
	if s.hooks != nil {
		_ = s.hooks.Emit(ctx, automation.TriggerDocumentUploaded, "document", d.ID, map[string]any{
			"name": d.Name, "leadId": d.LeadID, "dealId": d.DealID, "customerId": d.CustomerID,
		})
	}
	return d, nil
}

func (s *Service) Open(ctx context.Context, id string) (io.ReadCloser, *Document, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if d.FileKey == "" {
		return nil, nil, apperrors.Validation("document has no uploaded file")
	}
	rc, _, err := s.store.Open(ctx, d.FileKey)
	if err != nil {
		return nil, nil, apperrors.Internal("failed to open file", err)
	}
	return rc, d, nil
}

func (s *Service) Verify(ctx context.Context, actorID, id string, ip, ua string) (*Document, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status != StatusUploaded && current.Status != StatusRejected {
		return nil, apperrors.Validation("only uploaded or rejected documents can be verified")
	}
	if current.FileKey == "" {
		return nil, apperrors.Validation("cannot verify a document without a file")
	}
	d, err := s.repo.Verify(ctx, id, actorID)
	if err != nil {
		return nil, apperrors.Internal("failed to verify document", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "document.verified", "document", audit.Ptr(id), map[string]any{
		"name": d.Name,
	}, ip, ua)
	s.emit(ctx, actorID, timeline.EventDocumentVerified, "Document verified", d.Name, d)
	return d, nil
}

func (s *Service) Reject(ctx context.Context, actorID, id string, in RejectInput, ip, ua string) (*Document, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status != StatusUploaded && current.Status != StatusVerified {
		return nil, apperrors.Validation("only uploaded or verified documents can be rejected")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, apperrors.Validation("rejection reason is required")
	}
	d, err := s.repo.Reject(ctx, id, actorID, reason)
	if err != nil {
		return nil, apperrors.Internal("failed to reject document", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "document.rejected", "document", audit.Ptr(id), map[string]any{
		"name": d.Name, "reason": reason,
	}, ip, ua)
	s.emit(ctx, actorID, timeline.EventDocumentRejected, "Document rejected", d.Name+": "+reason, d)
	return d, nil
}

func (s *Service) Update(ctx context.Context, actorID, id string, in UpdateMetaInput, ip, ua string) (*Document, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var expires *time.Time
	setExpires := false
	if in.ExpiresAt != nil {
		setExpires = true
		expires, err = parseOptionalDate(in.ExpiresAt)
		if err != nil {
			return nil, apperrors.Validation("invalid expiryDate")
		}
	}
	if in.Status != nil {
		switch *in.Status {
		case StatusRequested, StatusUploaded, StatusVerified, StatusRejected, StatusMissing, StatusExpired:
		default:
			return nil, apperrors.Validation("invalid status")
		}
	}
	d, err := s.repo.UpdateMeta(ctx, id, current, in, expires, setExpires)
	if err != nil {
		return nil, apperrors.Internal("failed to update document", err)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "document.updated", "document", audit.Ptr(id), map[string]any{
		"name": d.Name, "status": d.Status,
	}, ip, ua)
	return d, nil
}

func (s *Service) Delete(ctx context.Context, actorID, id string, ip, ua string) error {
	d, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return apperrors.Internal("failed to delete document", err)
	}
	if d.FileKey != "" {
		_ = s.store.Delete(ctx, d.FileKey)
	}
	_ = s.audit.Record(ctx, audit.Ptr(actorID), "document.deleted", "document", audit.Ptr(id), map[string]any{
		"name": d.Name,
	}, ip, ua)
	return nil
}

func (s *Service) emit(ctx context.Context, actorID, eventType, title, body string, d *Document) {
	if s.timeline == nil || d == nil {
		return
	}
	_, _ = s.timeline.Record(ctx, timeline.WriteInput{
		EventType:   eventType,
		Title:       title,
		Body:        body,
		ActorUserID: audit.Ptr(actorID),
		Source:      "crm",
		LeadID:      d.LeadID,
		CustomerID:  d.CustomerID,
		DealID:      d.DealID,
		DocumentID:  &d.ID,
		Metadata: map[string]any{
			"documentId": d.ID, "name": d.Name, "type": d.DocType, "status": d.Status,
		},
	})
}

func parseOptionalDate(v *string) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	raw := strings.TrimSpace(*v)
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse("2006-01-02", raw)
		if err != nil {
			return nil, err
		}
	}
	utc := t.UTC()
	return &utc, nil
}
