package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/kjk/u"
)

// url: /{forum}/viewraw?topicId=${topicId}&postId=${postId}
func handleViewRaw(w http.ResponseWriter, r *http.Request) {
	forum, topicID, postID := getTopicAndPostID(w, r)
	if 0 == topicID {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}
	topic := forum.Store.TopicByID(topicID)
	if nil == topic {
		logger.Noticef("handleViewRaw(): didn't find topic with id %d, referer: %q", topicID, getReferer(r))
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}
	post := topic.Posts[postID-1]
	msg := bytesToPlane0String(post.Message)
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("****** Raw:\n"))
	w.Write([]byte(msg))
	w.Write([]byte("\n\n****** Converted:\n"))
	w.Write([]byte(msgToHtml(msg)))
}

func serveFileFromDir(w http.ResponseWriter, r *http.Request, dir, fileName string) {
	filePath := filepath.Join(dir, fileName)
	if !u.PathExists(filePath) {
		logger.Noticef("serveFileFromDir() file %q doesn't exist, referer: %q", fileName, getReferer(r))
	}
	http.ServeFile(w, r, filePath)
}

// url: /s/*
func handleStatic(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/s/"):]
	serveFileFromDir(w, r, "static", file)
}

// url: /img/*
func handleStaticImg(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/img/"):]
	serveFileFromDir(w, r, "img", file)
}

// url: /robots.txt
func handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	serveFileFromDir(w, r, "static", "robots.txt")
}

func getTopicAndPostID(w http.ResponseWriter, r *http.Request) (*Forum, int, int) {
	forum := mustGetForum(w, r)
	if forum == nil {
		http.Redirect(w, r, "/", 302)
		return nil, 0, 0
	}
	topicIDStr := strings.TrimSpace(r.FormValue("topicId"))
	postIDStr := strings.TrimSpace(r.FormValue("postId"))
	topicID, err := strconv.Atoi(topicIDStr)
	if err != nil || topicID == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return nil, 0, 0
	}
	postID, err := strconv.Atoi(postIDStr)
	if err != nil || postID == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return forum, 0, 0
	}
	return forum, topicID, postID
}

// url: /{forum}/postdel?topicId=${topicId}&postId=${postId}
func handlePostDelete(w http.ResponseWriter, r *http.Request) {
	if forum, topicID, postID := getTopicAndPostID(w, r); forum != nil {
		//fmt.Printf("handlePostDelete(): forum: %q, topicId: %d, postId: %d\n", forum.ForumUrl, topicId, postId)
		// TODO: handle error?
		forum.Store.DeletePost(topicID, postID)
		http.Redirect(w, r, fmt.Sprintf("/%s/topic?id=%d", forum.ForumUrl, topicID), 302)
	}
}

// url: /{forum}/postundel?topicId=${topicId}&postId=${postId}
func handlePostUndelete(w http.ResponseWriter, r *http.Request) {
	if forum, topicID, postID := getTopicAndPostID(w, r); forum != nil {
		//fmt.Printf("handlePostUndelete(): forum: %q, topicId: %d, postId: %d\n", forum.ForumUrl, topicId, postId)
		// TODO: handle error?
		forum.Store.UndeletePost(topicID, postID)
		http.Redirect(w, r, fmt.Sprintf("/%s/topic?id=%d", forum.ForumUrl, topicID), 302)
	}
}

func getIPAddr(w http.ResponseWriter, r *http.Request) (*Forum, string) {
	forum := mustGetForum(w, r)
	if forum == nil {
		http.Redirect(w, r, "/", 302)
		return nil, ""
	}
	ipAddr := strings.TrimSpace(r.FormValue("ip"))
	if ipAddr == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return nil, ""
	}
	return forum, ipAddr
}

// url: /{forum}/blockip?ip=${ip}
func handleBlockIP(w http.ResponseWriter, r *http.Request) {
	if forum, ip := getIPAddr(w, r); forum != nil {
		//fmt.Printf("handleBlockIP(): forum: %q, ip: %s\n", forum.ForumUrl, ip)
		forum.Store.BlockIP(ip)
		http.Redirect(w, r, fmt.Sprintf("/%s/postsby?ip=%s", forum.ForumUrl, ip), 302)
	}
}

// url: /{forum}/unblockip?ip=${ip}
func handleUnblockIP(w http.ResponseWriter, r *http.Request) {
	if forum, ip := getIPAddr(w, r); forum != nil {
		//fmt.Printf("handleUnblockIP(): forum: %q, ip: %s\n", forum.ForumUrl, ip)
		forum.Store.UnblockIP(ip)
		http.Redirect(w, r, fmt.Sprintf("/%s/postsby?ip=%s", forum.ForumUrl, ip), 302)
	}
}

// url: /
func handleMain(w http.ResponseWriter, r *http.Request) {
	if !isTopLevelURL(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	model := struct {
		Forums        *[]*Forum
		AnalyticsCode string
	}{
		Forums: &appState.Forums,
	}
	ExecTemplate(w, tmplMain, model)
}

// // https://blog.gopheracademy.com/advent-2016/exposing-go-on-the-internet/
func initHTTPServer() *http.Server {
	r := mux.NewRouter()
	r.HandleFunc("/", makeTimingHandler(handleMain))
	r.HandleFunc("/{forum}", makeTimingHandler(handleForum))
	r.HandleFunc("/{forum}/", makeTimingHandler(handleForum))
	r.HandleFunc("/{forum}/rss", makeTimingHandler(handleRss))
	r.HandleFunc("/{forum}/rssall", makeTimingHandler(handleRssAll))
	r.HandleFunc("/{forum}/topic", makeTimingHandler(handleTopic))
	r.HandleFunc("/{forum}/postsby", makeTimingHandler(handlePostsBy))
	r.HandleFunc("/{forum}/postdel", makeTimingHandler(handlePostDelete))
	r.HandleFunc("/{forum}/postundel", makeTimingHandler(handlePostUndelete))
	r.HandleFunc("/{forum}/viewraw", makeTimingHandler(handleViewRaw))
	r.HandleFunc("/{forum}/newpost", makeTimingHandler(handleNewPost))
	r.HandleFunc("/{forum}/blockip", makeTimingHandler(handleBlockIP))
	r.HandleFunc("/{forum}/unblockip", makeTimingHandler(handleUnblockIP))

	smux := &http.ServeMux{}
	smux.HandleFunc("/oauthgithubcb", handleOauthGithubCallback)
	smux.HandleFunc("/login", handleLogin)
	smux.HandleFunc("/logout", handleLogout)
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handleRobotsTxt)
	smux.HandleFunc("/logs", handleLogs)
	smux.HandleFunc("/s/", makeTimingHandler(handleStatic))
	smux.HandleFunc("/img/", makeTimingHandler(handleStaticImg))
	smux.Handle("/", r)

	srv := &http.Server{
		// TODO: 1.8 only
		// IdleTimeout:  120 * time.Second,
		Handler: smux,
	}
	// TODO: track connections and their state
	return srv
}
