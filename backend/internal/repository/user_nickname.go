package repository

import (
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/nickname"
)

var ErrNicknameLocked = errors.New("nickname already set")

func (r *UserRepository) MarkNicknamePrompted(userID int) error {
	result, err := r.db.Exec(`UPDATE users SET nickname_prompted_at=COALESCE(nickname_prompted_at, NOW()) WHERE id=$1`, userID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetNickname is atomic so concurrent devices cannot overwrite the first choice.
func (r *UserRepository) SetNickname(userID int, raw string) error {
	name, err := nickname.Validate(raw)
	if err != nil {
		return err
	}
	result, err := r.db.Exec(`UPDATE users SET nickname=$1 WHERE id=$2 AND nickname=''`, name, userID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNicknameLocked
	}
	return nil
}
