package repository

import (
	"database/sql"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
)

type TemplateRepository struct {
	db Querier
}

func NewTemplateRepository(db *sql.DB) *TemplateRepository {
	return &TemplateRepository{db: db}
}

// WithCtx returns a repository view bound to the per-request transaction
// stashed under ctxkey.Tx by RLSTxMiddleware. Falls back to the underlying
// handle if no tx is present.
func (r *TemplateRepository) WithCtx(c ctxkey.CtxGetter) *TemplateRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok {
			return &TemplateRepository{db: q}
		}
	}
	return r
}

// GetSystemTemplates 获取所有系统模板
func (r *TemplateRepository) GetSystemTemplates() ([]model.SummaryTemplate, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, name, description, style, brief_prompt, detailed_prompt, is_system, created_at
		 FROM summary_templates WHERE is_system = true ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTemplates(rows)
}

// GetUserTemplates 获取用户的自定义模板
func (r *TemplateRepository) GetUserTemplates(userID int) ([]model.SummaryTemplate, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, name, description, style, brief_prompt, detailed_prompt, is_system, created_at
		 FROM summary_templates WHERE is_system = false AND user_id = $1 ORDER BY id ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTemplates(rows)
}

// CreateUserTemplate 创建用户模板
func (r *TemplateRepository) CreateUserTemplate(t *model.SummaryTemplate) error {
	return r.db.QueryRow(
		`INSERT INTO summary_templates (user_id, name, description, style, brief_prompt, detailed_prompt, is_system)
		 VALUES ($1, $2, $3, $4, $5, $6, false)
		 RETURNING id, created_at`,
		t.UserID, t.Name, t.Description, t.Style, t.BriefPrompt, t.DetailedPrompt,
	).Scan(&t.ID, &t.CreatedAt)
}

// DeleteUserTemplate 删除用户模板（只能删自己的）
func (r *TemplateRepository) DeleteUserTemplate(id, userID int) error {
	_, err := r.db.Exec(
		`DELETE FROM summary_templates WHERE id = $1 AND user_id = $2 AND is_system = false`,
		id, userID,
	)
	return err
}

// GetByID 获取单个模板
func (r *TemplateRepository) GetByID(id int) (*model.SummaryTemplate, error) {
	t := &model.SummaryTemplate{}
	err := r.db.QueryRow(
		`SELECT id, user_id, name, description, style, brief_prompt, detailed_prompt, is_system, created_at
		 FROM summary_templates WHERE id = $1`,
		id,
	).Scan(&t.ID, &t.UserID, &t.Name, &t.Description, &t.Style, &t.BriefPrompt, &t.DetailedPrompt, &t.IsSystem, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// SetUserDefaultTemplate 更新用户默认模板（存在 users 表中）
func (r *TemplateRepository) SetUserDefaultTemplate(userID, templateID int) error {
	_, err := r.db.Exec(
		`UPDATE users SET default_template_id = $1 WHERE id = $2`,
		templateID, userID,
	)
	return err
}

func scanTemplates(rows *sql.Rows) ([]model.SummaryTemplate, error) {
	var templates []model.SummaryTemplate
	for rows.Next() {
		var t model.SummaryTemplate
		err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Description, &t.Style, &t.BriefPrompt, &t.DetailedPrompt, &t.IsSystem, &t.CreatedAt)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, nil
}
