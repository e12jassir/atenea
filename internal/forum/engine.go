package forum

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ForumEngine defines the interface for interacting with Moodle forum activities.
type ForumEngine interface {
	ListDiscussions(httpClient *http.Client, baseURL string, forumID string) ([]ForumDiscussion, error)
	GetDiscussionPosts(httpClient *http.Client, baseURL string, discussionID string) ([]ForumPost, error)
	ReplyToPost(httpClient *http.Client, baseURL string, sesskey string, req ReplyRequest) error
}

// DefaultForumEngine implements the ForumEngine interface.
type DefaultForumEngine struct{}

// NewEngine creates a new instance of ForumEngine.
func NewEngine() ForumEngine {
	return &DefaultForumEngine{}
}

// ListDiscussions fetches and parses the discussions from /mod/forum/view.php?id=<forum_id>.
func (e *DefaultForumEngine) ListDiscussions(httpClient *http.Client, baseURL string, forumID string) ([]ForumDiscussion, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("baseURL cannot be empty")
	}
	forumID = strings.TrimSpace(forumID)
	if forumID == "" {
		return nil, errors.New("forumID cannot be empty")
	}

	cleanBase := strings.TrimRight(baseURL, "/")
	viewURL := fmt.Sprintf("%s/mod/forum/view.php?id=%s", cleanBase, url.QueryEscape(forumID))

	req, err := http.NewRequest("GET", viewURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Atenea/2.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error connecting to forum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error listing discussions: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading forum response: %w", err)
	}

	return parseDiscussions(string(bodyBytes), cleanBase), nil
}

// GetDiscussionPosts fetches and parses all posts from /mod/forum/discuss.php?d=<discussion_id>.
func (e *DefaultForumEngine) GetDiscussionPosts(httpClient *http.Client, baseURL string, discussionID string) ([]ForumPost, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("baseURL cannot be empty")
	}
	discussionID = strings.TrimSpace(discussionID)
	if discussionID == "" {
		return nil, errors.New("discussionID cannot be empty")
	}

	cleanBase := strings.TrimRight(baseURL, "/")
	discussURL := fmt.Sprintf("%s/mod/forum/discuss.php?d=%s", cleanBase, url.QueryEscape(discussionID))

	req, err := http.NewRequest("GET", discussURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Atenea/2.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error connecting to discussion: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching discussion posts: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading discussion response: %w", err)
	}

	return parseDiscussionPosts(string(bodyBytes), discussionID), nil
}

// ReplyToPost submits a reply to an existing forum post.
func (e *DefaultForumEngine) ReplyToPost(httpClient *http.Client, baseURL string, sesskey string, req ReplyRequest) error {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return errors.New("baseURL cannot be empty")
	}
	req.ParentPostID = strings.TrimSpace(req.ParentPostID)
	if req.ParentPostID == "" {
		return errors.New("parentPostID cannot be empty")
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return errors.New("message cannot be empty")
	}

	cleanBase := strings.TrimRight(baseURL, "/")
	replyURL := fmt.Sprintf("%s/mod/forum/post.php?reply=%s", cleanBase, url.QueryEscape(req.ParentPostID))

	// Step 1: GET reply form to extract dynamic fields, sesskey, and itemid
	getReq, err := http.NewRequest("GET", replyURL, nil)
	if err != nil {
		return fmt.Errorf("error creating GET reply form request: %w", err)
	}
	getReq.Header.Set("User-Agent", "Atenea/2.0")
	getReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := httpClient.Do(getReq)
	if err != nil {
		return fmt.Errorf("error fetching reply form: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error fetching reply form: HTTP %d", resp.StatusCode)
	}

	formBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading reply form: %w", err)
	}
	formHTML := string(formBytes)

	hiddenFields, extractedSesskey, extractedItemID, defaultSubject := extractFormFields(formHTML)

	// Determine effective sesskey
	effectiveSesskey := strings.TrimSpace(sesskey)
	if extractedSesskey != "" {
		effectiveSesskey = extractedSesskey
	}
	if effectiveSesskey == "" {
		return errors.New("could not extract sesskey from reply form and none was provided")
	}

	// Determine effective itemid
	effectiveItemID := extractedItemID
	if effectiveItemID == "" {
		effectiveItemID = fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond))
	}

	// Determine subject
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		if defaultSubject != "" {
			subject = defaultSubject
		} else {
			subject = fmt.Sprintf("Re: Post %s", req.ParentPostID)
		}
	}

	// Step 2: Build POST form values
	data := url.Values{}
	for k, v := range hiddenFields {
		data.Set(k, v)
	}

	data.Set("_qf__mod_forum_post_form", "1")
	data.Set("sesskey", effectiveSesskey)
	data.Set("reply", req.ParentPostID)
	data.Set("subject", subject)
	data.Set("message[text]", req.Message)
	data.Set("message[format]", "1")
	data.Set("message[itemid]", effectiveItemID)
	if req.CourseID != "" {
		data.Set("course", req.CourseID)
	}
	if req.ForumID != "" {
		data.Set("forum", req.ForumID)
	}
	if req.DiscussionID != "" {
		data.Set("discussion", req.DiscussionID)
	}
	if req.ParentPostID != "" {
		data.Set("parent", req.ParentPostID)
	}
	if _, ok := data["submitbutton"]; !ok {
		data.Set("submitbutton", "Post to forum")
	}

	// Step 3: POST to /mod/forum/post.php
	postURL := fmt.Sprintf("%s/mod/forum/post.php", cleanBase)
	postReq, err := http.NewRequest("POST", postURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("error creating POST reply request: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", "Atenea/2.0")
	postReq.Header.Set("Referer", replyURL)

	postResp, err := httpClient.Do(postReq)
	if err != nil {
		return fmt.Errorf("error posting reply: %w", err)
	}
	defer postResp.Body.Close()

	// Step 4: Validate response code (200 OK or 3xx redirect)
	statusCode := postResp.StatusCode
	isRedirect := statusCode == http.StatusSeeOther || statusCode == http.StatusFound ||
		statusCode == http.StatusMovedPermanently || statusCode == http.StatusTemporaryRedirect

	if isRedirect {
		return nil
	}

	if statusCode == http.StatusOK {
		respBodyBytes, _ := io.ReadAll(postResp.Body)
		respBody := string(respBodyBytes)

		reFormError := regexp.MustCompile(`(?i)(?:class=['"][^'"]*(?:alert-danger|form-item-error)[^'"]*['"]|id=['"]id_error_[^'"]*['"]|<span class=['"]error['"]>)`)
		if reFormError.MatchString(respBody) {
			reErrorMsg := regexp.MustCompile(`(?is)<(?:span|div)\b[^>]*class=['"][^'"]*(?:error|alert-danger)[^'"]*['"][^>]*>(.*?)</(?:span|div)>`)
			errMsg := "form submission returned validation error"
			if em := reErrorMsg.FindStringSubmatch(respBody); len(em) >= 2 {
				cleanMsg := cleanHTML(em[1])
				if cleanMsg != "" {
					errMsg = cleanMsg
				}
			}
			return fmt.Errorf("moodle rejected reply: %s", errMsg)
		}
		return nil
	}

	return fmt.Errorf("reply failed with HTTP status %d", statusCode)
}

// parseDiscussions extracts discussion metadata from the Moodle view.php HTML.
func parseDiscussions(htmlContent string, baseURL string) []ForumDiscussion {
	cleanBase := strings.TrimRight(baseURL, "/")
	var discussions []ForumDiscussion

	reRow := regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	rows := reRow.FindAllStringSubmatch(htmlContent, -1)

	reLink := regexp.MustCompile(`(?is)<a\b[^>]+href=['"]([^'"]*discuss\.php\?d=(\d+)[^'"]*)['"][^>]*>(.*?)</a>`)
	reAuthorTd := regexp.MustCompile(`(?is)<td\b[^>]*class=['"][^'"]*author[^'"]*['"][^>]*>(.*?)</td>`)
	reRepliesTd := regexp.MustCompile(`(?is)<td\b[^>]*class=['"][^'"]*replies[^'"]*['"][^>]*>(.*?)</td>`)
	reLastpostTd := regexp.MustCompile(`(?is)<td\b[^>]*class=['"][^'"]*lastpost[^'"]*['"][^>]*>(.*?)</td>`)
	reTime := regexp.MustCompile(`(?is)<time\b[^>]*>(.*?)</time>`)
	reAnchor := regexp.MustCompile(`(?is)<a\b[^>]*>(.*?)</a>`)
	reDigits := regexp.MustCompile(`\d+`)
	reLocked := regexp.MustCompile(`(?i)(class=['"][^'"]*\blocked\b[^'"]*['"]|<i\b[^>]+fa-lock|title=['"][^'"]*(?:locked|bloquead)[^'"]*['"]|aria-label=['"][^'"]*(?:locked|bloquead)[^'"]*['"]|alt=['"][^'"]*(?:locked|bloquead)[^'"]*['"])`)

	for _, rowMatch := range rows {
		rowHTML := rowMatch[1]
		linkMatch := reLink.FindStringSubmatch(rowHTML)
		if len(linkMatch) < 4 {
			continue
		}

		link := linkMatch[1]
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			link = cleanBase + "/" + strings.TrimLeft(link, "/")
		}
		id := linkMatch[2]
		title := cleanHTML(linkMatch[3])

		author := ""
		date := ""
		replyCount := 0
		isLocked := reLocked.MatchString(rowMatch[0])

		// Parse author
		authorMatch := reAuthorTd.FindStringSubmatch(rowHTML)
		if len(authorMatch) >= 2 {
			authorHTML := authorMatch[1]
			if am := reAnchor.FindStringSubmatch(authorHTML); len(am) >= 2 {
				author = cleanHTML(am[1])
			} else {
				author = cleanHTML(authorHTML)
			}

			if tm := reTime.FindStringSubmatch(authorHTML); len(tm) >= 2 {
				date = cleanHTML(tm[1])
			}
		}

		// Parse date if not found in author
		if date == "" {
			if tm := reTime.FindStringSubmatch(rowHTML); len(tm) >= 2 {
				date = cleanHTML(tm[1])
			} else if lpMatch := reLastpostTd.FindStringSubmatch(rowHTML); len(lpMatch) >= 2 {
				date = cleanHTML(lpMatch[1])
			}
		}

		// Parse reply count
		repliesMatch := reRepliesTd.FindStringSubmatch(rowHTML)
		if len(repliesMatch) >= 2 {
			if digits := reDigits.FindString(repliesMatch[1]); digits != "" {
				replyCount, _ = strconv.Atoi(digits)
			}
		}

		discussions = append(discussions, ForumDiscussion{
			ID:         id,
			Title:      title,
			Author:     author,
			Date:       date,
			ReplyCount: replyCount,
			Link:       link,
			IsLocked:   isLocked,
		})
	}

	// Fallback if no table rows matched
	if len(discussions) == 0 {
		seen := make(map[string]bool)
		linkMatches := reLink.FindAllStringSubmatch(htmlContent, -1)
		for _, lm := range linkMatches {
			if len(lm) < 4 {
				continue
			}
			id := lm[2]
			if seen[id] {
				continue
			}
			seen[id] = true
			link := lm[1]
			if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
				link = cleanBase + "/" + strings.TrimLeft(link, "/")
			}
			discussions = append(discussions, ForumDiscussion{
				ID:    id,
				Title: cleanHTML(lm[3]),
				Link:  link,
			})
		}
	}

	return discussions
}

// parseDiscussionPosts extracts posts from discuss.php HTML.
func parseDiscussionPosts(htmlContent string, discussionID string) []ForumPost {
	var posts []ForumPost

	rePostStart := regexp.MustCompile(`(?is)<(?:article|div)\b[^>]*\bclass=['"][^'"]*forumpost[^'"]*['"][^>]*>`)
	locs := rePostStart.FindAllStringIndex(htmlContent, -1)

	if len(locs) == 0 {
		rePostStartAlt := regexp.MustCompile(`(?is)<(?:article|div)\b[^>]*\bid=['"]p\d+['"][^>]*>`)
		locs = rePostStartAlt.FindAllStringIndex(htmlContent, -1)
	}

	if len(locs) == 0 {
		return posts
	}

	reID := regexp.MustCompile(`(?i)\bid=['"]p?(\d+)['"]`)
	reDataID := regexp.MustCompile(`(?i)\bdata-post-id=['"](\d+)['"]`)
	reReplyLink := regexp.MustCompile(`(?is)post\.php\?reply=(\d+)`)
	reParentLink := regexp.MustCompile(`(?is)(?:discuss\.php\?d=\d+(?:#p|&amp;parent=)|#p)(\d+)['"][^>]*>(?:[^<]*(?:padre|parent|in reply)[^<]*)</a>`)
	reParentData := regexp.MustCompile(`(?i)\bdata-parent-id=['"](\d+)['"]`)
	reParentParam := regexp.MustCompile(`(?i)\bparent=(\d+)`)

	reSubject := regexp.MustCompile(`(?is)<(?:h[1-6]|div)\b[^>]*class=['"][^'"]*subject[^'"]*['"][^>]*>(.*?)</(?:h[1-6]|div)>`)
	reAuthor := regexp.MustCompile(`(?is)<(?:div|span)\b[^>]*class=['"][^'"]*author[^'"]*['"][^>]*>(.*?)</(?:div|span)>`)
	reAuthorLink := regexp.MustCompile(`(?is)<a\b[^>]*href=['"][^'"]*user/view\.php[^'"]*['"][^>]*>(.*?)</a>`)
	reTime := regexp.MustCompile(`(?is)<time\b[^>]*>(.*?)</time>`)

	for i := 0; i < len(locs); i++ {
		startIdx := locs[i][0]
		endIdx := len(htmlContent)
		if i+1 < len(locs) {
			endIdx = locs[i+1][0]
		}
		chunk := htmlContent[startIdx:endIdx]

		postID := ""
		startTag := htmlContent[locs[i][0]:locs[i][1]]
		if idMatch := reID.FindStringSubmatch(startTag); len(idMatch) >= 2 {
			postID = idMatch[1]
		} else if idMatch := reDataID.FindStringSubmatch(startTag); len(idMatch) >= 2 {
			postID = idMatch[1]
		} else if idMatch := reID.FindStringSubmatch(chunk); len(idMatch) >= 2 {
			postID = idMatch[1]
		} else if replyMatch := reReplyLink.FindStringSubmatch(chunk); len(replyMatch) >= 2 {
			postID = replyMatch[1]
		}

		parentID := ""
		if pm := reParentData.FindStringSubmatch(chunk); len(pm) >= 2 {
			parentID = pm[1]
		} else if pm := reParentLink.FindStringSubmatch(chunk); len(pm) >= 2 {
			parentID = pm[1]
		} else if pm := reParentParam.FindStringSubmatch(chunk); len(pm) >= 2 {
			parentID = pm[1]
		}

		subject := ""
		if sm := reSubject.FindStringSubmatch(chunk); len(sm) >= 2 {
			subject = cleanHTML(sm[1])
		}

		author := ""
		date := ""
		if am := reAuthor.FindStringSubmatch(chunk); len(am) >= 2 {
			authorHTML := am[1]
			if alm := reAuthorLink.FindStringSubmatch(authorHTML); len(alm) >= 2 {
				author = cleanHTML(alm[1])
			} else {
				author = cleanHTML(authorHTML)
			}
			if tm := reTime.FindStringSubmatch(authorHTML); len(tm) >= 2 {
				date = cleanHTML(tm[1])
			}
		} else if alm := reAuthorLink.FindStringSubmatch(chunk); len(alm) >= 2 {
			author = cleanHTML(alm[1])
		}

		if date == "" {
			if tm := reTime.FindStringSubmatch(chunk); len(tm) >= 2 {
				date = cleanHTML(tm[1])
			}
		}

		contentHTML := extractBalancedDiv(chunk, `(?is)<div\b[^>]*class=['"][^'"]*(?:post-content-container|posting)[^'"]*['"][^>]*>`)
		if contentHTML == "" {
			reContentFallback := regexp.MustCompile(`(?is)<div\b[^>]*class=['"][^'"]*(?:post-content-container|posting|content)[^'"]*['"][^>]*>(.*?)</div>`)
			if cm := reContentFallback.FindStringSubmatch(chunk); len(cm) >= 2 {
				contentHTML = cm[1]
			}
		}
		contentHTML = strings.TrimSpace(contentHTML)
		contentText := cleanHTML(contentHTML)

		posts = append(posts, ForumPost{
			ID:           postID,
			ParentID:     parentID,
			DiscussionID: discussionID,
			Author:       author,
			Date:         date,
			Subject:      subject,
			ContentHTML:  contentHTML,
			ContentText:  contentText,
		})
	}

	return posts
}

// extractFormFields extracts hidden fields, sesskey, itemid, and default subject from reply form HTML.
func extractFormFields(formHTML string) (hiddenFields map[string]string, sesskey string, itemid string, defaultSubject string) {
	hiddenFields = make(map[string]string)

	reInput := regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	matches := reInput.FindAllStringSubmatch(formHTML, -1)

	reName := regexp.MustCompile(`(?i)\bname=['"]([^'"]+)['"]`)
	reValue := regexp.MustCompile(`(?i)\bvalue=['"]([^'"]*)['"]`)
	reType := regexp.MustCompile(`(?i)\btype=['"]([^'"]+)['"]`)

	for _, m := range matches {
		attrStr := m[1]
		nameMatch := reName.FindStringSubmatch(attrStr)
		valueMatch := reValue.FindStringSubmatch(attrStr)
		typeMatch := reType.FindStringSubmatch(attrStr)

		if len(nameMatch) < 2 {
			continue
		}
		name := nameMatch[1]
		value := ""
		if len(valueMatch) >= 2 {
			value = html.UnescapeString(valueMatch[1])
		}
		inputType := "text"
		if len(typeMatch) >= 2 {
			inputType = strings.ToLower(typeMatch[1])
		}

		if inputType == "hidden" {
			hiddenFields[name] = value
		}

		if name == "sesskey" && sesskey == "" {
			sesskey = value
		}
		if (name == "message[itemid]" || name == "itemid") && itemid == "" {
			itemid = value
		}
		if name == "subject" && defaultSubject == "" {
			defaultSubject = value
		}
	}

	// Fallback for sesskey in script or config
	if sesskey == "" {
		reSess := regexp.MustCompile(`['"]sesskey['"]\s*:\s*['"]([a-zA-Z0-9]+)['"]`)
		if sm := reSess.FindStringSubmatch(formHTML); len(sm) >= 2 {
			sesskey = sm[1]
		}
	}

	// Fallback for itemid in script or config
	if itemid == "" {
		reItem := regexp.MustCompile(`['"]itemid['"]\s*:\s*['"]?(\d+)['"]?`)
		if im := reItem.FindStringSubmatch(formHTML); len(im) >= 2 {
			itemid = im[1]
		}
	}

	return hiddenFields, sesskey, itemid, defaultSubject
}

// extractBalancedDiv extracts the inner HTML of a div matching openingPattern, accounting for nested divs.
func extractBalancedDiv(htmlSnippet string, openingPattern string) string {
	reStart := regexp.MustCompile(openingPattern)
	loc := reStart.FindStringIndex(htmlSnippet)
	if loc == nil {
		return ""
	}
	startContent := loc[1]
	rest := htmlSnippet[startContent:]

	depth := 1
	i := 0
	for i < len(rest) {
		lower := strings.ToLower(rest[i:])
		if strings.HasPrefix(lower, "<div") {
			endTag := strings.Index(rest[i:], ">")
			if endTag != -1 {
				depth++
				i += endTag + 1
				continue
			}
		} else if strings.HasPrefix(lower, "</div>") {
			depth--
			if depth == 0 {
				return rest[:i]
			}
			i += 6
			continue
		}
		i++
	}

	// Fallback
	reFallback := regexp.MustCompile(`(?is)` + openingPattern + `(.*?)</div>`)
	if m := reFallback.FindStringSubmatch(htmlSnippet); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// cleanHTML strips tags, script, style, and converts block breaks to newlines.
func cleanHTML(s string) string {
	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	s = reScript.ReplaceAllString(s, "")
	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	s = reStyle.ReplaceAllString(s, "")

	reBreak := regexp.MustCompile(`(?i)<br\s*/?>`)
	s = reBreak.ReplaceAllString(s, "\n")
	reBlock := regexp.MustCompile(`(?i)</(?:p|div|tr|h[1-6]|li|blockquote)>`)
	s = reBlock.ReplaceAllString(s, "\n")

	reTag := regexp.MustCompile(`<[^>]+>`)
	s = reTag.ReplaceAllString(s, " ")

	s = html.UnescapeString(s)

	lines := strings.Split(s, "\n")
	var cleaned []string
	reSpaces := regexp.MustCompile(`[ \t\r\f\v]+`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			trimmed = reSpaces.ReplaceAllString(trimmed, " ")
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.Join(cleaned, "\n")
}
