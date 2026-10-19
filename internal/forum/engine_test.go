package forum

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListDiscussions_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mod/forum/view.php", func(w http.ResponseWriter, r *http.Request) {
		forumID := r.URL.Query().Get("id")
		if forumID != "7955069" {
			http.Error(w, "forum not found", http.StatusNotFound)
			return
		}

		htmlResp := `<!DOCTYPE html>
<html>
<head><title>Foro Tematico</title></head>
<body>
<div class="discussion-list">
  <table class="generaltable forumheaderlist">
    <thead>
      <tr>
        <th>Debate</th>
        <th>Comenzado por</th>
        <th>Respuestas</th>
        <th>Ultimo mensaje</th>
      </tr>
    </thead>
    <tbody>
      <tr class="discussion">
        <th class="topic starter">
          <a href="https://zajuna.sena.edu.co/zajuna/mod/forum/discuss.php?d=101">Foro Temático 1: Clean Code</a>
        </th>
        <td class="author">
          <a href="/user/view.php?id=10">Profesor Mario</a>
          <time datetime="2026-10-18">18 Oct 2026</time>
        </td>
        <td class="replies">
          <a href="#">5</a>
        </td>
        <td class="lastpost">19 Oct 2026</td>
      </tr>
      <tr class="discussion locked">
        <th class="topic starter">
          <a href="/mod/forum/discuss.php?d=102">Foro Social y Presentacion</a>
          <i class="fa fa-lock" title="Bloqueado"></i>
        </th>
        <td class="author">
          <a href="/user/view.php?id=20">Ana Torres</a>
        </td>
        <td class="replies">0</td>
        <td class="lastpost">12 Oct 2026</td>
      </tr>
    </tbody>
  </table>
</div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlResp))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	engine := NewEngine()
	discussions, err := engine.ListDiscussions(server.Client(), server.URL, "7955069")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(discussions) != 2 {
		t.Fatalf("expected 2 discussions, got %d", len(discussions))
	}

	// Discussion 1 check
	d1 := discussions[0]
	if d1.ID != "101" {
		t.Errorf("expected ID 101, got %q", d1.ID)
	}
	if d1.Title != "Foro Temático 1: Clean Code" {
		t.Errorf("expected title 'Foro Temático 1: Clean Code', got %q", d1.Title)
	}
	if d1.Author != "Profesor Mario" {
		t.Errorf("expected author 'Profesor Mario', got %q", d1.Author)
	}
	if d1.Date != "18 Oct 2026" {
		t.Errorf("expected date '18 Oct 2026', got %q", d1.Date)
	}
	if d1.ReplyCount != 5 {
		t.Errorf("expected ReplyCount 5, got %d", d1.ReplyCount)
	}
	if d1.Link != "https://zajuna.sena.edu.co/zajuna/mod/forum/discuss.php?d=101" {
		t.Errorf("expected absolute link, got %q", d1.Link)
	}
	if d1.IsLocked {
		t.Errorf("expected d1 not locked, got locked")
	}

	// Discussion 2 check
	d2 := discussions[1]
	if d2.ID != "102" {
		t.Errorf("expected ID 102, got %q", d2.ID)
	}
	if d2.Title != "Foro Social y Presentacion" {
		t.Errorf("expected title 'Foro Social y Presentacion', got %q", d2.Title)
	}
	if d2.Author != "Ana Torres" {
		t.Errorf("expected author 'Ana Torres', got %q", d2.Author)
	}
	if d2.ReplyCount != 0 {
		t.Errorf("expected ReplyCount 0, got %d", d2.ReplyCount)
	}
	expectedD2Link := server.URL + "/mod/forum/discuss.php?d=102"
	if d2.Link != expectedD2Link {
		t.Errorf("expected link %q, got %q", expectedD2Link, d2.Link)
	}
	if !d2.IsLocked {
		t.Errorf("expected d2 to be locked, got not locked")
	}
}

func TestListDiscussions_FallbackCardLayout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mod/forum/view.php", func(w http.ResponseWriter, r *http.Request) {
		htmlResp := `<div class="card">
			<a href="/mod/forum/discuss.php?d=301">Debate General</a>
		</div>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlResp))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	engine := NewEngine()
	discussions, err := engine.ListDiscussions(server.Client(), server.URL, "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(discussions) != 1 {
		t.Fatalf("expected 1 discussion from fallback, got %d", len(discussions))
	}
	if discussions[0].ID != "301" {
		t.Errorf("expected ID 301, got %q", discussions[0].ID)
	}
	if discussions[0].Title != "Debate General" {
		t.Errorf("expected title 'Debate General', got %q", discussions[0].Title)
	}
}

func TestListDiscussions_Errors(t *testing.T) {
	engine := NewEngine()

	// Empty baseURL
	if _, err := engine.ListDiscussions(nil, "", "123"); err == nil {
		t.Error("expected error for empty baseURL, got nil")
	}

	// Empty forumID
	if _, err := engine.ListDiscussions(nil, "http://localhost", ""); err == nil {
		t.Error("expected error for empty forumID, got nil")
	}

	// Server error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := engine.ListDiscussions(server.Client(), server.URL, "123"); err == nil {
		t.Error("expected error for HTTP 500, got nil")
	}
}

func TestGetDiscussionPosts_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mod/forum/discuss.php", func(w http.ResponseWriter, r *http.Request) {
		discID := r.URL.Query().Get("d")
		if discID != "101" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		htmlResp := `<!DOCTYPE html>
<html>
<head><title>Discusion 101</title></head>
<body>
<div class="discussioncontrols"></div>
<article id="p501" class="forumpost focus-control" data-post-id="501" aria-label="Tema: Principios SOLID">
  <div class="row header">
    <h3 class="subject">Tema: Principios SOLID</h3>
    <div class="author">
      <a href="/user/view.php?id=10">Instructor Sena</a>
      <time datetime="2026-10-18T08:00:00Z">18 Oct 2026, 08:00</time>
    </div>
  </div>
  <div class="row maincontent">
    <div class="post-content-container">
      <p>Apreciados aprendices,<br>Favor compartir sus dudas sobre el principio de <strong>Inversión de Dependencias</strong>.</p>
    </div>
  </div>
  <div class="commands">
    <a href="/mod/forum/post.php?reply=501">Responder</a>
  </div>
</article>

<article id="p502" class="forumpost" data-post-id="502" data-parent-id="501" aria-label="Re: Tema: Principios SOLID">
  <div class="row header">
    <h3 class="subject">Re: Tema: Principios SOLID</h3>
    <div class="author">
      <a href="/user/view.php?id=20">Esteban Marrugo</a>
      <time datetime="2026-10-18T09:30:00Z">18 Oct 2026, 09:30</time>
    </div>
  </div>
  <div class="row maincontent">
    <div class="post-content-container">
      <p>Buenas noches instructor,<br>Aquí presento mi solución aplicando <code>Go interfaces</code>:</p>
      <div class="codeblock"><pre>type Engine interface {}</pre></div>
    </div>
  </div>
  <div class="commands">
    <a href="/mod/forum/discuss.php?d=101#p501">Mostrar mensaje anterior</a>
    <a href="/mod/forum/post.php?reply=502">Responder</a>
  </div>
</article>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlResp))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	engine := NewEngine()
	posts, err := engine.GetDiscussionPosts(server.Client(), server.URL, "101")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(posts) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(posts))
	}

	// Post 1
	p1 := posts[0]
	if p1.ID != "501" {
		t.Errorf("expected ID 501, got %q", p1.ID)
	}
	if p1.ParentID != "" {
		t.Errorf("expected empty ParentID for root post, got %q", p1.ParentID)
	}
	if p1.DiscussionID != "101" {
		t.Errorf("expected DiscussionID 101, got %q", p1.DiscussionID)
	}
	if p1.Subject != "Tema: Principios SOLID" {
		t.Errorf("expected subject 'Tema: Principios SOLID', got %q", p1.Subject)
	}
	if p1.Author != "Instructor Sena" {
		t.Errorf("expected author 'Instructor Sena', got %q", p1.Author)
	}
	if p1.Date != "18 Oct 2026, 08:00" {
		t.Errorf("expected date '18 Oct 2026, 08:00', got %q", p1.Date)
	}
	if !strings.Contains(p1.ContentHTML, "<strong>Inversión de Dependencias</strong>") {
		t.Errorf("expected HTML content to contain strong tag, got %q", p1.ContentHTML)
	}
	if !strings.Contains(p1.ContentText, "Inversión de Dependencias") {
		t.Errorf("expected Text content to contain clean text, got %q", p1.ContentText)
	}

	// Post 2
	p2 := posts[1]
	if p2.ID != "502" {
		t.Errorf("expected ID 502, got %q", p2.ID)
	}
	if p2.ParentID != "501" {
		t.Errorf("expected ParentID 501, got %q", p2.ParentID)
	}
	if p2.DiscussionID != "101" {
		t.Errorf("expected DiscussionID 101, got %q", p2.DiscussionID)
	}
	if p2.Subject != "Re: Tema: Principios SOLID" {
		t.Errorf("expected subject 'Re: Tema: Principios SOLID', got %q", p2.Subject)
	}
	if p2.Author != "Esteban Marrugo" {
		t.Errorf("expected author 'Esteban Marrugo', got %q", p2.Author)
	}
	if p2.Date != "18 Oct 2026, 09:30" {
		t.Errorf("expected date '18 Oct 2026, 09:30', got %q", p2.Date)
	}
	if !strings.Contains(p2.ContentHTML, "type Engine interface {}") {
		t.Errorf("expected HTML content to contain nested code div, got %q", p2.ContentHTML)
	}
	if !strings.Contains(p2.ContentText, "type Engine interface {}") {
		t.Errorf("expected Text content to contain code text, got %q", p2.ContentText)
	}
}

func TestGetDiscussionPosts_Errors(t *testing.T) {
	engine := NewEngine()

	// Empty baseURL
	if _, err := engine.GetDiscussionPosts(nil, "", "101"); err == nil {
		t.Error("expected error for empty baseURL, got nil")
	}

	// Empty discussionID
	if _, err := engine.GetDiscussionPosts(nil, "http://localhost", ""); err == nil {
		t.Error("expected error for empty discussionID, got nil")
	}

	// 404 response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	if _, err := engine.GetDiscussionPosts(server.Client(), server.URL, "101"); err == nil {
		t.Error("expected error for HTTP 404, got nil")
	}
}

func TestReplyToPost_SuccessRedirect303(t *testing.T) {
	getRequested := false
	postRequested := false

	mux := http.NewServeMux()
	mux.HandleFunc("/mod/forum/post.php", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getRequested = true
			replyTo := r.URL.Query().Get("reply")
			if replyTo != "501" {
				http.Error(w, "invalid reply id", http.StatusBadRequest)
				return
			}

			formHTML := `<!DOCTYPE html>
<html>
<body>
<form action="/mod/forum/post.php" method="post" id="mform1">
  <input type="hidden" name="_qf__mod_forum_post_form" value="1">
  <input type="hidden" name="sesskey" value="sesskey_token_12345">
  <input type="hidden" name="message[itemid]" value="987654321">
  <input type="hidden" name="reply" value="501">
  <input type="hidden" name="forum" value="7955069">
  <input type="hidden" name="discussion" value="101">
  <input type="text" name="subject" value="Re: Tema: Principios SOLID">
  <textarea name="message[text]"></textarea>
  <input type="submit" name="submitbutton" value="Enviar al foro">
</form>
</body>
</html>`
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(formHTML))
			return
		}

		if r.Method == http.MethodPost {
			postRequested = true
			if err := r.ParseForm(); err != nil {
				http.Error(w, "failed to parse form", http.StatusBadRequest)
				return
			}

			// Validate all required Moodle POST fields
			if r.FormValue("_qf__mod_forum_post_form") != "1" {
				t.Errorf("expected _qf__mod_forum_post_form=1, got %q", r.FormValue("_qf__mod_forum_post_form"))
			}
			if r.FormValue("sesskey") != "sesskey_token_12345" {
				t.Errorf("expected sesskey=sesskey_token_12345, got %q", r.FormValue("sesskey"))
			}
			if r.FormValue("message[itemid]") != "987654321" {
				t.Errorf("expected message[itemid]=987654321, got %q", r.FormValue("message[itemid]"))
			}
			if r.FormValue("reply") != "501" {
				t.Errorf("expected reply=501, got %q", r.FormValue("reply"))
			}
			if r.FormValue("subject") != "Re: Mi respuesta a SOLID" {
				t.Errorf("expected subject 'Re: Mi respuesta a SOLID', got %q", r.FormValue("subject"))
			}
			if r.FormValue("message[text]") != "Este es el contenido de mi aporte técnico." {
				t.Errorf("expected message[text] matches, got %q", r.FormValue("message[text]"))
			}
			if r.FormValue("message[format]") != "1" {
				t.Errorf("expected message[format]=1, got %q", r.FormValue("message[format]"))
			}

			// Respond with 303 redirect
			http.Redirect(w, r, "/mod/forum/discuss.php?d=101#p503", http.StatusSeeOther)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/mod/forum/discuss.php", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>Discussion page after redirect</body></html>"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	engine := NewEngine()
	req := ReplyRequest{
		CourseID:     "125540",
		ForumID:      "7955069",
		DiscussionID: "101",
		ParentPostID: "501",
		Subject:      "Re: Mi respuesta a SOLID",
		Message:      "Este es el contenido de mi aporte técnico.",
	}

	// Test with client following redirects (standard)
	err := engine.ReplyToPost(server.Client(), server.URL, "", req)
	if err != nil {
		t.Fatalf("unexpected error with default redirect follower: %v", err)
	}

	if !getRequested || !postRequested {
		t.Errorf("expected both GET and POST requests, got get=%v, post=%v", getRequested, postRequested)
	}

	// Test with client that does NOT follow redirects (stopping at 303)
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	err = engine.ReplyToPost(noRedirectClient, server.URL, "", req)
	if err != nil {
		t.Fatalf("unexpected error when stopping at 303: %v", err)
	}
}

func TestReplyToPost_SuccessDirect200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mod/forum/post.php", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			formHTML := `<form action="/mod/forum/post.php" method="post">
				<input type="hidden" name="sesskey" value="xyz987">
				<input type="hidden" name="itemid" value="112233">
			</form>`
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(formHTML))
			return
		}

		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Post successful</body></html>"))
			return
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	engine := NewEngine()
	req := ReplyRequest{
		ParentPostID: "501",
		Message:      "Mensaje directo 200",
	}

	err := engine.ReplyToPost(server.Client(), server.URL, "fallback_sesskey", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReplyToPost_FormError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mod/forum/post.php", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			formHTML := `<form action="/mod/forum/post.php" method="post">
				<input type="hidden" name="sesskey" value="token123">
			</form>`
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(formHTML))
			return
		}

		if r.Method == http.MethodPost {
			// Simulate validation error re-rendering form
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body>
				<div class="alert alert-danger">Debe ingresar un mensaje válido</div>
			</body></html>`))
			return
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	engine := NewEngine()
	req := ReplyRequest{
		ParentPostID: "501",
		Message:      "test",
	}

	err := engine.ReplyToPost(server.Client(), server.URL, "", req)
	if err == nil {
		t.Fatal("expected error due to moodle form validation error, got nil")
	}
	if !strings.Contains(err.Error(), "Debe ingresar un mensaje válido") {
		t.Errorf("expected error message to contain validation error text, got %q", err.Error())
	}
}

func TestReplyToPost_ValidationInputs(t *testing.T) {
	engine := NewEngine()

	// Empty baseURL
	if err := engine.ReplyToPost(nil, "", "token", ReplyRequest{ParentPostID: "1", Message: "m"}); err == nil {
		t.Error("expected error for empty baseURL, got nil")
	}

	// Empty parentPostID
	if err := engine.ReplyToPost(nil, "http://localhost", "token", ReplyRequest{ParentPostID: "", Message: "m"}); err == nil {
		t.Error("expected error for empty parentPostID, got nil")
	}

	// Empty message
	if err := engine.ReplyToPost(nil, "http://localhost", "token", ReplyRequest{ParentPostID: "1", Message: ""}); err == nil {
		t.Error("expected error for empty message, got nil")
	}

	// Server returning 500 on GET form
	serverGet500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer serverGet500.Close()

	if err := engine.ReplyToPost(serverGet500.Client(), serverGet500.URL, "token", ReplyRequest{ParentPostID: "1", Message: "m"}); err == nil {
		t.Error("expected error on GET form 500, got nil")
	}

	// Server without sesskey in form and no sesskey provided
	serverNoSesskey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<form></form>"))
	}))
	defer serverNoSesskey.Close()

	if err := engine.ReplyToPost(serverNoSesskey.Client(), serverNoSesskey.URL, "", ReplyRequest{ParentPostID: "1", Message: "m"}); err == nil {
		t.Error("expected error when no sesskey is found anywhere, got nil")
	}
}

func TestCleanHTML(t *testing.T) {
	input := `<style>body{color:red;}</style>
	<script>console.log("hello");</script>
	<p>Primer p&aacute;rrafo con <strong>negrita</strong> y &amp; s&iacute;mbolos.<br>Segunda l&iacute;nea.</p>
	<div>Tercer bloque.</div>`

	cleaned := cleanHTML(input)
	if strings.Contains(cleaned, "body{color:red;}") {
		t.Error("cleanHTML should have removed <style> block")
	}
	if strings.Contains(cleaned, "console.log") {
		t.Error("cleanHTML should have removed <script> block")
	}
	if !strings.Contains(cleaned, "Primer párrafo con negrita y & símbolos.") {
		t.Errorf("cleanHTML did not unescape HTML properly: %q", cleaned)
	}
	if !strings.Contains(cleaned, "Segunda línea.") {
		t.Errorf("cleanHTML did not preserve br newline: %q", cleaned)
	}
	if !strings.Contains(cleaned, "Tercer bloque.") {
		t.Errorf("cleanHTML did not include div text: %q", cleaned)
	}
}

func TestExtractFormFields(t *testing.T) {
	htmlContent := `
	<input type="hidden" name="sesskey" value="test_sess">
	<input type="hidden" name="message[itemid]" value="12345678">
	<input type="hidden" name="_qf__mod_forum_post_form" value="1">
	<input type="text" name="subject" value="Re: Asunto Original">
	`

	hidden, sesskey, itemid, subject := extractFormFields(htmlContent)
	if sesskey != "test_sess" {
		t.Errorf("expected sesskey 'test_sess', got %q", sesskey)
	}
	if itemid != "12345678" {
		t.Errorf("expected itemid '12345678', got %q", itemid)
	}
	if subject != "Re: Asunto Original" {
		t.Errorf("expected subject 'Re: Asunto Original', got %q", subject)
	}
	if hidden["_qf__mod_forum_post_form"] != "1" {
		t.Errorf("expected hidden _qf__mod_forum_post_form=1, got %q", hidden["_qf__mod_forum_post_form"])
	}
}
