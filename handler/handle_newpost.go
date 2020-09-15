// This code is in Public Domain. Take all the code you want, I'll just write more.
package handler

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/fofou/common"
	"github.com/coyove/fofou/server"
)

var rxImageExts = regexp.MustCompile(`(?i)(\.png|\.jpg|\.jpeg|\.gif|\.svg)$`)

func PostAPI(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(common.Kforum.MaxImageSize)*1024*1024)

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	badRequest := func() { writeSimpleJSON(w, "success", false, "error", "bad-request") }
	internalError := func() { writeSimpleJSON(w, "success", false, "error", "internal-error") }

	var topic server.Topic

	topicID, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("topic")))
	if topicID > 0 {
		if topic = common.Kforum.Store.GetTopic(uint32(topicID), server.DefaultTopicMapper); topic.ID == 0 {
			common.Kforum.Notice("invalid topic ID: %d\n", topicID)
			badRequest()
			return
		}
	}

	ipAddr, user := getIPAddress(r), common.Kforum.GetUser(r)

	if !user.Can(server.PERM_ADMIN) {
		if common.Kforum.Store.IsBlocked(ipAddr) {
			common.Kforum.Notice("blocked a post from IP: %v", ipAddr)
			badRequest()
			return
		}
		if common.Kforum.Store.IsBlocked(user.ID) {
			common.Kforum.Notice("blocked a post from user %v", user.ID)
			badRequest()
			return
		}
		if !user.CanModerate() && !throtNewPost(ipAddr, user.ID) {
			badRequest()
			return
		}
	}

	if !strings.HasPrefix(r.Referer(), common.Kforum.URL) && common.Kprod {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !user.IsValid() {
		if common.Kforum.NoMoreNewUsers && !topic.FreeReply {
			writeSimpleJSON(w, "success", false, "error", "no-more-new-users")
			return
		}
		copy(user.ID[2:], common.Kforum.Rand.Fetch(6))
		user.T = time.Now().Unix()
		if topic.ID == 0 {
			user.N = uint32(common.Kforum.Rand.Intn(10) + 10)
		} else {
			user.N = uint32(common.Kforum.Rand.Intn(5) + 5)
		}
	}

	// if user didn't pass the dice test, we will challenge him/her
	if false && !user.Can(server.PERM_NO_ROLL) && !user.PassRoll() {
		_testCount, _ := common.KbadUsers.Get(user.ID)
		testCount, _ := _testCount.(int)
		if testCount++; testCount > 10 {
			common.KbadUsers.Remove(user.ID)
			common.Kforum.Block(user.ID)
			common.Kforum.Block(ipAddr)
			badRequest()
			return
		}

		recaptcha := strings.TrimSpace(r.FormValue("token"))
		if recaptcha == "" {
			writeSimpleJSON(w, "success", false, "error", "recaptcha-needed")
			common.KbadUsers.Add(user.ID, testCount)
			return
		}

		resp, err := (&http.Client{Timeout: time.Second * 5}).PostForm("https://www.recaptcha.net/recaptcha/api/siteverify", url.Values{
			"secret":   []string{common.Kforum.RecaptchaSecret},
			"response": []string{recaptcha},
		})
		if err != nil {
			common.Kforum.Error("recaptcha error: %v", err)
			internalError()
			return
		}

		defer resp.Body.Close()
		buf, _ := ioutil.ReadAll(resp.Body)

		recaptchaResult := map[string]interface{}{}
		json.Unmarshal(buf, &recaptchaResult)

		if r, _ := recaptchaResult["success"].(bool); !r {
			common.Kforum.Error("recaptcha failed: %v", string(buf))
			common.KbadUsers.Add(user.ID, testCount)
			writeSimpleJSON(w, "success", false, "error", "recaptcha-failed")
			return
		}
	}
	common.KbadUsers.Remove(user.ID)

	subject := strings.Replace(r.FormValue("subject"), "<", "&lt;", -1)
	msg := r.FormValue("message")
	options := r.FormValue("options")
	sage := strings.Contains(options, "sage")
	nsfw := strings.Contains(options, "nsfw")
	nocookie := strings.Contains(options, "nocookie")

	if strings.HasPrefix(subject, "!!") {
		topic.ID = 0
	}

	// validate the fields
	if !user.Can(server.PERM_ADMIN) && strings.Contains(msg, "```") {
		msg = reMessage.ReplaceAllString(msg, "```")
	}

	if modCode(common.Kforum, user, subject, msg) {
		_, username := server.Format8Bytes(user.ID)
		ipstr, _ := server.Format8Bytes(ipAddr)
		common.Kforum.Notice("mod %s from %s has performed: %s", username, ipstr, msg)
		writeSimpleJSON(w, "success", true, "mod-operation", msg)
		return
	}

	// simple mechanism to prevent double post only
	uuid := server.DecodeUUID(r.FormValue("uuid"))
	if _, existed := common.Kuuids.Get(uuid); existed {
		badRequest()
		return
	}
	common.Kuuids.Add(uuid, true)

	if topic.ID == 0 {
		if tmp := []rune(subject); len(tmp) > common.Kforum.MaxSubjectLen {
			tmp[common.Kforum.MaxSubjectLen-1], tmp[common.Kforum.MaxSubjectLen-2], tmp[common.Kforum.MaxSubjectLen-3] = '.', '.', '.'
			subject = string(tmp[:common.Kforum.MaxSubjectLen])
		}
	}

	image, imageInfo, err := r.FormFile("image")
	if err == nil {
		defer image.Close()
	}

	if err != nil && !strings.Contains(err.Error(), "no such file") {
		writeSimpleJSON(w, "success", false, "error", "image-upload-failed")
		return
	}

	if image != nil && imageInfo != nil && common.Kforum.NoImageUpload {
		writeSimpleJSON(w, "success", false, "error", "image-upload-disabled")
		return
	}

	if len(msg) > common.Kforum.MaxMessageLen {
		// hard trunc
		msg = msg[:common.Kforum.MaxMessageLen]
	}

	if len(msg) < common.Kforum.MinMessageLen && image == nil {
		writeSimpleJSON(w, "success", false, "error", "message-too-short")
		return
	}

	if topic.ID > 0 && topic.Locked {
		writeSimpleJSON(w, "success", false, "error", "topic-locked")
		return
	}

	if !nocookie {
		common.Kforum.SetUser(w, user)
	}

	var aImage *server.Image
	if image != nil && imageInfo != nil {
		aImage = &server.Image{}

		ext, hash := strings.ToLower(filepath.Ext(imageInfo.Filename)), sha1.Sum([]byte(imageInfo.Filename))
		if !rxImageExts.MatchString(ext) {
			writeSimpleJSON(w, "success", false, "error", "image-invalid-format")
			return
		}

		t := time.Now().Format("2006-Jan/02-15h")
		aImage.Name = sanitizeFilename(imageInfo.Filename)
		aImage.Path = fmt.Sprintf("%s/%s_%x%s", t, aImage.Name, hash[:4], ext)
		os.MkdirAll(common.DATA_IMAGES+t, 0755)

		of, err := os.Create(common.DATA_IMAGES + aImage.Path)
		if err != nil {
			writeSimpleJSON(w, "success", false, "error", "image-disk-error")
			common.Kforum.Error("copy image to dest: %v", err)
			return
		}

		nw, _ := io.Copy(of, image)
		aImage.Size = uint32(nw)
		common.Kiq.Push(common.DATA_IMAGES + aImage.Path)
		of.Close()
	}

	var postLongID uint64
	if topic.ID == 0 {
		postLongID, err = common.Kforum.Store.NewTopic(subject, msg, aImage, user.ID, ipAddr, sage)
		if err != nil {
			common.Kforum.Error("failed to create new topic: %v", err)
			internalError()
			return
		}
		if nsfw {
			common.Kforum.Store.FlagPost(user, postLongID, server.OP_NSFW, func(p *server.Post) {
				p.T_SetStatus(server.POST_T_ISNSFW)
			})
		}
		if common.Kforum.Rand.Intn(64) == 0 || (!common.Kprod && common.Kforum.Rand.Intn(3) == 0) {
			go func() {
				start := time.Now()
				common.Kforum.ArchiveJob()
				common.Kforum.Notice("archive threads in %.2fs", time.Since(start).Seconds())
			}()
		}
	} else {
		postLongID, err = common.Kforum.Store.NewPost(topic.ID, msg, aImage, user.ID, ipAddr, sage)
		if err != nil {
			common.Kforum.Error("failed to create new post to %d: %v", topic.ID, err)
			internalError()
			return
		}
	}

	tmpt, tmpp := server.SplitID(postLongID)
	writeSimpleJSON(w, "success", true, "topic", tmpt, "post", tmpp, "longid", postLongID)
}
