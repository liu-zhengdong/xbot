package bot

import "errors"

// ApplyAvatarChange reuses the profile resource write seam and reports completed
// avatar_file, avatar_mime and avatar metadata stages even when later IO fails.
// Upload and clear are independent actions; profile fields use ProfileChange.
func (b *Bot) ApplyAvatarChange(data []byte, clear bool) (ProfileChangeResult, error) {
	if clear {
		if len(data) != 0 {
			return ProfileChangeResult{Name: b.Name(), PreviousName: b.Name(), AppliedFields: []string{}}, errors.New("bot: avatar upload and clear are mutually exclusive")
		}
		return b.applyProfileResult(ProfileUpdate{}, avatarEdit{kind: clearAvatar})
	}
	return b.applyProfileResult(ProfileUpdate{}, avatarEdit{kind: uploadAvatar, data: data})
}
