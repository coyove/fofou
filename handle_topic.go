// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type PostDisplay struct {
	Post
	UserHomepage string
	MessageHtml  template.HTML
	CssClass     string
}

func formatPostCreatedOnTime(t time.Time) string {
	s := t.Format("January 2, 2006")
	return s
}

func (p *PostDisplay) CreatedOnStr() string {
	return formatPostCreatedOnTime(p.CreatedOn)
}

func NewPostDisplay(p *Post, forum *Forum, isAdmin bool) *PostDisplay {
	if p.IsDeleted && !isAdmin {
		return nil
	}

	pd := &PostDisplay{
		Post:     *p,
		CssClass: "post",
	}
	if p.IsDeleted {
		pd.CssClass = "post deleted"
	}
	msgHtml := msgToHtml(bytesToPlane0String(p.Message))
	pd.MessageHtml = template.HTML(msgHtml)

	if p.IsGithubUser() {
		pd.UserHomepage = "https://github.com/" + p.UserName()
	}

	if forum.ForumUrl == "sumatrapdf" {
		// backwards-compatibility hack for posts imported from old version of
		// fofou: hyper-link my name to my website
		if p.UserName() == "Krzysztof Kowalczyk" {
			pd.UserHomepage = "http://blog.kowalczyk.info"
		}
	}
	return pd
}

// TODO: this is simplistic but work for me, http://net.tutsplus.com/tutorials/other/8-regular-expressions-you-should-know/
// has more elaborate regex for extracting urls
var urlRx = regexp.MustCompile(`https?://[[:^space:]]+`)
var notUrlEndChars = []byte(".),")

func notUrlEndChar(c byte) bool {
	return -1 != bytes.IndexByte(notUrlEndChars, c)
}

var disableUrlization = false

func msgToHtml(s string) string {
	matches := urlRx.FindAllStringIndex(s, -1)
	if nil == matches || disableUrlization {
		s = template.HTMLEscapeString(s)
		s = strings.Replace(s, "\n", "<br>", -1)
		return s
	}

	urlMap := make(map[string]string)
	ns := ""
	prevEnd := 0
	for n, match := range matches {
		start, end := match[0], match[1]
		for end > start && notUrlEndChar(s[end-1]) {
			end -= 1
		}
		url := s[start:end]
		ns += s[prevEnd:start]

		// placeHolder is meant to be an unlikely string that doesn't exist in
		// the message, so that we can replace the string with it and then
		// revert the replacement. A more robust approach would be to remember
		// offsets
		placeHolder, ok := urlMap[url]
		if !ok {
			placeHolder = fmt.Sprintf("a;dfsl;a__lkasjdfh1234098;lajksdf_%d", n)
			urlMap[url] = placeHolder
		}
		ns += placeHolder
		prevEnd = end
	}
	ns += s[prevEnd:len(s)]

	ns = template.HTMLEscapeString(ns)
	for url, placeHolder := range urlMap {
		url = fmt.Sprintf(`<a href="%s" rel="nofollow">%s</a>`, url, url)
		ns = strings.Replace(ns, placeHolder, url, -1)
	}
	ns = strings.Replace(ns, "\n", "<br>", -1)
	return ns
}

func getLogInOut(r *http.Request, c *SecureCookieValue) template.HTML {
	redirectUrl := template.HTMLEscapeString(r.URL.String())
	s := ""
	if c.GithubUser == "" {
		s = `<span style="float: right;">Not logged in. <a href="/login?redirect=%s">Log in with Twitter</a></span>`
		s = fmt.Sprintf(s, redirectUrl)
	} else {
		s = `<span style="float:right;">Logged in as %s (<a href="/logout?redirect=%s">logout</a>)</span>`
		s = fmt.Sprintf(s, c.GithubUser, redirectUrl)
	}
	return template.HTML(s)
}

// url: /{forum}/topic?id=${id}
func handleTopic(w http.ResponseWriter, r *http.Request) {
	forum := mustGetForum(w, r)
	if forum == nil {
		return
	}
	idStr := strings.TrimSpace(r.FormValue("id"))
	topicID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}

	//fmt.Printf("handleTopic(): forum: %q, topicId: %d\n", forum.ForumUrl, topicId)
	topic := forum.Store.TopicByID(topicID)
	if nil == topic {
		logger.Noticef("handleTopic(): didn't find topic with id %d, referer: %q", topicID, getReferer(r))
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}

	isAdmin := userIsAdmin(forum, getSecureCookie(r))
	if topic.IsDeleted() && !isAdmin {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}

	posts := make([]*PostDisplay, 0)
	for _, p := range topic.Posts {
		pd := NewPostDisplay(&p, forum, isAdmin)
		if pd != nil {
			posts = append(posts, pd)
		}
	}

	sidebar := DoSidebarTemplate(forum, isAdmin)
	model := struct {
		Forum
		Topic
		SidebarHtml   template.HTML
		Posts         []*PostDisplay
		IsAdmin       bool
		AnalyticsCode *string
		LogInOut      template.HTML
	}{
		Forum:       *forum,
		Topic:       *topic,
		SidebarHtml: template.HTML(sidebar),
		Posts:       posts,
		IsAdmin:     isAdmin,
		LogInOut:    getLogInOut(r, getSecureCookie(r)),
	}
	ExecTemplate(w, tmplTopic, model)
}
