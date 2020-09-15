// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/common/lru"
	"github.com/coyove/fofou/common"
	"github.com/coyove/fofou/handler"
	"github.com/coyove/fofou/server"
)

const testPassword = "passw0rd"

var (
	listen   = flag.String("addr", ":5010", "HTTP server address")
	makeID   = flag.String("make", "", "Make ID, format: ID,MASK")
	snapshot = flag.String("ss", "", "Make snapshot of main.txt")
	csrf     = flag.String("csrf", "", "Change the URL for CSRF protection")
	salt     = flag.String("s", testPassword, "A secret string used as both salt and admin password")
)

func newForum(logger *server.Logger) *server.Forum {
	forum := &server.Forum{Logger: logger}

	start := time.Now()
	forum.Store = server.NewStore(common.DATA_MAIN,
		(&server.ForumConfig{}).SetSalt(*salt),
		func(store *server.Store) {
			forum.ForumConfig = &server.ForumConfig{}
			store.GetConfig(forum.ForumConfig)
			forum.ForumConfig.CorrectValues()
			forum.ForumConfig.Invalidate = time.Now().Unix()
			forum.SetSalt(*salt)

			rbuf, _ := ioutil.ReadFile(common.DATA_RECAPTCHA)
			rparts := strings.Split(string(rbuf), "|")
			if len(rparts) == 2 {
				forum.RecaptchaToken = rparts[0]
				forum.RecaptchaSecret = rparts[1]
				forum.Notice("recaptcha token: %s, secret: %s", forum.RecaptchaToken, forum.RecaptchaSecret)
			}
		})

	common.KbadUsers = lru.NewCache(1024)
	common.Kuuids = lru.NewCache(1024)
	common.Karchive = lru.NewCache(256)
	common.KthrotIPID = lru.NewCache(256)

	go func() {
		for {
			time.Sleep(100 * time.Millisecond)
			if forum.Store.IsReady() {
				vt, p := forum.PostsCount()
				forum.Notice("%d topics, %d live topics = %d, %d posts", forum.TopicsCount(), forum.LiveTopicsNum, vt, p)
				forum.Notice("loaded all in %.2fs", time.Now().Sub(start).Seconds())

				if *snapshot != "" {
					server.SnapshotStore(*snapshot, forum.Store)
					os.Exit(0)
				}

				if *csrf != "" {
					forum.ForumConfig.URL = *csrf
					forum.Store.UpdateConfig(forum.ForumConfig)
					os.Exit(0)
				}

				break
			}
		}
	}()
	return forum
}

var version string = "_devel_"

func preHandle(fn func(http.ResponseWriter, *http.Request), footer bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !common.Kforum.IsReady() {
			w.Write([]byte(fmt.Sprintf("%v Booting... %.1f%%", time.Now().Format(time.RFC1123), common.Kforum.LoadingProgress()*100)))
			return
		}

		ww := &server.ResponseWriterWrapper{w, http.StatusOK, false, nil}
		if !common.Kprod {
			common.Kforum.Invalidate = time.Now().Unix()
		}

		startTime := time.Now()
		fn(ww, r)
		duration := time.Since(startTime)

		if (footer || ww.ForceFooter) && ww.Code == http.StatusOK {
			server.Render(w, server.TmplFooter, struct {
				RenderTime  int64
				RunningTime string
			}{duration.Nanoseconds() / 1e6, version})
		}
		if duration.Seconds() > 0.1 {
			url := r.URL.Path
			if len(r.URL.RawQuery) > 0 {
				url = fmt.Sprintf("%s?%s", url, r.URL.RawQuery)
			}
			common.Kforum.Notice("%q took %fs to serve", url, duration.Seconds())
		}
	}
}

func main() {
	os.MkdirAll(common.DATA_IMAGES, 0755)
	os.MkdirAll(common.DATA_LOGS, 0755)

	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()
	logger := server.NewLogger(1024, 1024, true, common.DATA_LOGS+"f2")
	common.Kpassword = *salt

	if *salt == testPassword {
		logger.Notice("you are using the test password/salt, fofou will run in test mode")
	} else {
		logger.Notice("production mode on")
		logger.UseStdout = true
		common.Kprod = true
	}

	if *makeID != "" {
		u, parts := server.User{}, strings.Split(*makeID, ",")
		copy(u.ID[:], parts[0])
		m, _ := strconv.Atoi(parts[1])
		u.M = byte(m)

		forum := &server.Forum{}
		forum.ForumConfig = &server.ForumConfig{}
		forum.ForumConfig.SetSalt(*salt)
		forum.SetUser(nil, u)
		return
	}

	common.Kforum = newForum(logger)
	common.Kiq = server.NewImageQueue(logger, 200, runtime.NumCPU())

	server.LoadTemplates(common.Kprod)

	smux := &http.ServeMux{}
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handler.RobotsTxt)
	smux.HandleFunc("/mod", preHandle(handler.Mod, true))
	smux.HandleFunc("/cookie", preHandle(handler.Cookie, false))
	smux.HandleFunc("/s/", preHandle(handler.Static, false))
	smux.HandleFunc("/status", preHandle(handler.Help, true))
	smux.HandleFunc("/i/", preHandle(handler.Image, false))
	smux.HandleFunc("/api", preHandle(handler.PostAPI, false))
	smux.HandleFunc("/list", preHandle(handler.List, true))
	smux.HandleFunc("/rss.xml", preHandle(handler.RSS, false))
	smux.HandleFunc("/data.bin", preHandle(handler.Help, false))
	smux.HandleFunc("/t/", preHandle(handler.Topic, true))
	smux.HandleFunc("/p/", preHandle(handler.Post, false))
	smux.HandleFunc("/tagged", preHandle(handler.Topics, true))
	smux.HandleFunc("/", preHandle(handler.Topics, true))

	srv := &http.Server{Handler: smux}
	srv.Addr = *listen
	logger.Notice("running on %s", srv.Addr)

	go func() {
		for {
			if !common.Kforum.Store.IsReady() {
				time.Sleep(time.Second)
				continue
			}

			start := time.Now()
			if err := common.Kforum.Store.Dup(common.DATA_MAIN + ".snapshot"); err != nil {
				logger.Error("failed to dup the store: %v", err)
			}
			logger.Notice("dup the store in %.2fs", time.Since(start).Seconds())

			if common.Kprod {
				time.Sleep(time.Hour * 6)
			} else {
				time.Sleep(time.Minute)
			}
		}
	}()

	go func() {
		for range time.Tick(time.Minute) {
		}
	}()

	common.Kstart = time.Now()
	if err := srv.ListenAndServe(); err != nil {
		fmt.Println("http.ListendAndServe() failed with %s", err)
	}
}
