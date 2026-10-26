package repository

import "errors"

var ErrNicknameLocked = errors.New("nickname already set")

// SetNickname is atomic so concurrent devices cannot overwrite the first choice.
func (r *UserRepository) SetNickname(userID int, nickname string) error {
	result, err := r.db.Exec(`UPDATE users SET nickname=$1 WHERE id=$2 AND nickname=''`, nickname, userID)
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
