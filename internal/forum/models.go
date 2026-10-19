package forum

// ForumDiscussion represents a discussion thread in a Moodle forum.
type ForumDiscussion struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Date       string `json:"date"`
	ReplyCount int    `json:"reply_count"`
	Link       string `json:"link"`
	IsLocked   bool   `json:"is_locked"`
}

// ForumPost represents an individual post/message within a discussion thread.
type ForumPost struct {
	ID           string `json:"id"`
	ParentID     string `json:"parent_id,omitempty"`
	DiscussionID string `json:"discussion_id"`
	Author       string `json:"author"`
	Date         string `json:"date"`
	Subject      string `json:"subject"`
	ContentHTML  string `json:"content_html"`
	ContentText  string `json:"content_text"`
}

// ReplyRequest contains the parameters needed to submit a reply to a forum post.
type ReplyRequest struct {
	CourseID     string `json:"course_id,omitempty"`
	ForumID      string `json:"forum_id,omitempty"`
	DiscussionID string `json:"discussion_id,omitempty"`
	ParentPostID string `json:"parent_post_id"`
	Subject      string `json:"subject"`
	Message      string `json:"message"`
}
