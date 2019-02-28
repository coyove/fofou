// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/common/rand"
)

// ModelNewPost represents a new post
type ModelNewPost struct {
	*Forum
	TopicID        int
	PrevCaptcha    string
	PrevSubject    string
	Token          string
	SubjectError   bool
	MessageError   bool
	TokenError     bool
	TopicLocked    bool
	NoMoreNewUsers bool
	PrevMessage    string
	NameClass      string
	PrevName       string
}

var randG = rand.New()

func getIPAddress(r *http.Request) (v [8]byte) {
	ipAddr := ""
	hdrRealIP, hdrForwardedFor := r.Header.Get("X-Real-Ip"), r.Header.Get("X-Forwarded-For")

	if hdrRealIP == "" && hdrForwardedFor == "" {
		s := r.RemoteAddr
		idx := strings.LastIndex(s, ":")
		if idx == -1 {
			ipAddr = s
		} else {
			ipAddr = s[:idx]
		}
	} else if hdrForwardedFor != "" {
		parts := strings.Split(hdrForwardedFor, ",")
		ipAddr = strings.TrimSpace(parts[0])
	} else {
		ipAddr = hdrRealIP
	}

	ip := net.ParseIP(ipAddr)
	if len(ip) == 0 {
		return
	}
	ipv4 := ip.To4()
	if len(ipv4) == 0 {
		copy(v[:], ip)
		return
	}
	copy(v[4:], ipv4[:3])
	return
}

func writeSimpleJSON(w http.ResponseWriter, args ...interface{}) {
	var p bytes.Buffer
	p.WriteString("{")
	for i := 0; i < len(args); i += 2 {
		k, _ := args[i].(string)
		p.WriteByte('"')
		p.WriteString(k)
		p.WriteString(`":`)
		buf, _ := json.Marshal(args[i+1])
		p.Write(buf)
		p.WriteByte(',')
	}
	if len(args) > 0 {
		p.Truncate(p.Len() - 1)
	}
	p.WriteString("}")
	w.Write(p.Bytes())
}

func createNewPost(forum *Forum, topic *Topic, w http.ResponseWriter, r *http.Request) {

}

func handleNewPost(w http.ResponseWriter, r *http.Request) {
	badRequest := func() { writeSimpleJSON(w, "success", false, "error", "bad-request") }
	internalError := func() { writeSimpleJSON(w, "success", false, "error", "internal-error") }

	var topic *Topic

	topicID, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("topic")))
	if topicID > 0 {
		if topic = forum.Store.TopicByID(uint32(topicID)); topic == nil {
			logger.Noticef("handleNewPost(): invalid topic ID: %d\n", topicID)
			badRequest()
			return
		}
	}

	ipAddr := getIPAddress(r)
	user := getUser(r)
	if forum.Store.IsBlocked(ipAddr) {
		logger.Noticef("blocked a post from IP: %s", ipAddr)
		badRequest()
		return
	}

	if !user.noTest {
		recaptcha := strings.TrimSpace(r.FormValue("token"))
		if recaptcha == "" {
			writeSimpleJSON(w, "success", false, "error", "recaptcha-needed")
			return
		}

		resp, err := (&http.Client{Timeout: time.Second * 5}).PostForm("https://www.recaptcha.net/recaptcha/api/siteverify", url.Values{
			"secret":   []string{forum.Recaptcha},
			"response": []string{recaptcha},
		})
		if err != nil {
			logger.Errorf("recaptcha error: %v", err)
			internalError()
			return
		}

		defer resp.Body.Close()
		buf, _ := ioutil.ReadAll(resp.Body)

		recaptchaResult := map[string]interface{}{}
		json.Unmarshal(buf, &recaptchaResult)

		if r, _ := recaptchaResult["success"].(bool); !r {
			logger.Errorf("recaptcha failed: %v", string(buf))
			writeSimpleJSON(w, "success", false, "error", "recaptcha-failed")
			return
		}
	}

	subject := strings.Replace(r.FormValue("subject"), "<", "&lt;", -1)
	msg := strings.TrimSpace(r.FormValue("message"))

	// validate the fields
	if !user.IsValid() {
		if forum.NoMoreNewUsers && !topic.FreeReply {
			writeSimpleJSON(w, "success", false, "error", "no-more-new-users")
			return
		}
		copy(user.ID[:], randG.Fetch(6))
		if user.ID[1] == ':' {
			user.ID[1]++
		}
	}

	if forum.IsAdmin(user.ID) && adminOpCode(forum, msg) {
		writeSimpleJSON(w, "success", true, "admin-operation", msg)
		return
	}

	if topic == nil {
		if tmp := []rune(subject); len(tmp) > forum.MaxSubjectLen {
			tmp[forum.MaxSubjectLen-1], tmp[forum.MaxSubjectLen-2], tmp[forum.MaxSubjectLen-3] = '.', '.', '.'
			subject = string(tmp[:forum.MaxSubjectLen])
		}
	}

	if len(msg) > forum.MaxMessageLen {
		// hard trunc
		msg = msg[:forum.MaxMessageLen]
	}

	if len(msg) < forum.MinMessageLen {
		writeSimpleJSON(w, "success", false, "error", "message-too-short")
		return
	}

	if topic != nil && topic.Locked {
		writeSimpleJSON(w, "success", false, "error", "topic-locked")
		return
	}

	setUser(w, user)

	if forum.Store.IsBlocked(user.ID) {
		logger.Noticef("blocked a post from user %s", user.ID)
		badRequest()
		return
	}

	image, _, err := r.FormFile("image")
	if err != nil {
		defer image.Close()
	}

	if topic == nil {
		topicID, err := forum.Store.CreateNewTopic(subject, msg, user.ID, ipAddr)
		if err != nil {
			logger.Errorf("createNewPost(): store.CreateNewPost() failed with %s", err)
			internalError()
			return
		}
		writeSimpleJSON(w, "success", true, "topic", topicID)
		return
	}

	if err := forum.Store.AddPostToTopic(topic.ID, msg, user.ID, ipAddr); err != nil {
		logger.Errorf("createNewPost(): store.AddPostToTopic() failed with %s", err)
		internalError()
		return
	}
	writeSimpleJSON(w, "success", true, "topic", topic.ID)
}
