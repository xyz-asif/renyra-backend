package models

// PublicProfileResponse — returned for any user's public profile
type PublicProfileResponse struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	Username       string `json:"username"`
	PhotoURL       string `json:"photoURL"`
	CoverImageURL  string `json:"coverImageURL"`
	Bio            string `json:"bio"`
	ExternalLink   string `json:"externalLink"`
	IsEditor       bool   `json:"isEditor"`
	PostsCount     int    `json:"postsCount"`
	FollowersCount int    `json:"followersCount"`
	FollowingCount int    `json:"followingCount"`
	IsFollowedByMe bool   `json:"isFollowedByMe"` // true if the caller follows this user
	IsMe           bool   `json:"isMe"`           // true if caller == this user
}

// FeedPage — paginated poem feed response
type FeedPage struct {
	Poems   []PoemResponse `json:"poems"`
	HasMore bool           `json:"hasMore"`
}

// UserSearchResult — user result in search
type UserSearchResult struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
	PhotoURL    string `json:"photoURL"`
	IsEditor    bool   `json:"isEditor"`
	IsFollowing bool   `json:"isFollowing"` // true if caller follows this user
}

// UserSearchPage — paginated user search response
type UserSearchPage struct {
	Users   []UserSearchResult `json:"users"`
	HasMore bool               `json:"hasMore"`
}

// PoemSearchPage — paginated poem search response
type PoemSearchPage struct {
	Poems   []PoemResponse `json:"poems"`
	HasMore bool           `json:"hasMore"`
}
