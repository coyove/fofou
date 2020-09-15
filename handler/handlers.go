// This code is in Public Domain. Take all the code you want, I'll just write more.
package handler

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/coyove/fofou/common"
	"github.com/coyove/fofou/server"
)

func serveFileFromDir(w http.ResponseWriter, r *http.Request, dir, fileName string) {
	filePath := filepath.Join(dir, fileName)
	http.ServeFile(w, r, filePath)
}

// url: /s/*
func Static(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/s/"):]
	serveFileFromDir(w, r, "static", file)
}

func Image(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/i/"):]
	path = strings.Replace(path, "..", "", -1)
	file := filepath.Join(common.DATA_IMAGES, path)

	if rxImageExts.MatchString(file) {
		if r.FormValue("thumb") == "1" && !strings.HasSuffix(file, ".svg") {
			path := file + ".thumb.jpg"
			if _, err := os.Stat(path); err == nil {
				http.ServeFile(w, r, path)
				return
			}
			common.Kiq.Push(file)
		}

		fi, _ := os.Stat(file)
		if fi != nil {
		}
		http.ServeFile(w, r, file)
		return
	}

	files, _ := ioutil.ReadDir(file)
	type _file struct {
		Name  string
		Path  string
		IsDir bool
		Time  uint32
		Size  uint64
	}

	p := struct {
		server.Forum
		Files []_file
		Up    string
		Path  string
	}{
		Forum: *common.Kforum,
		Path:  path,
		Up:    filepath.Dir(path),
	}

	p.Files = make([]_file, 0, len(files))
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".thumb.jpg") {
			continue
		}

		p.Files = append(p.Files, _file{
			Name:  file.Name(),
			Path:  filepath.Join(path, file.Name()),
			IsDir: file.IsDir(),
			Size:  uint64(file.Size()),
			Time:  uint32(file.ModTime().Unix()),
		})
	}

	sort.Slice(p.Files, func(i, j int) bool {
		// directories come first, then sorted by mod time
		ii, jj := uint64(p.Files[i].Time), uint64(p.Files[j].Time)
		if p.Files[i].IsDir {
			ii <<= 32
		}
		if p.Files[j].IsDir {
			jj <<= 32
		}
		return ii > jj
	})

	server.Render(w, server.TmplBrowser, p)
	w.(*server.ResponseWriterWrapper).ForceFooter = true
}

func Help(w http.ResponseWriter, r *http.Request) {
	path := "data/main.txt.snapshot"
	if r.RequestURI == "/data.bin" {
		http.ServeFile(w, r, path)
		return
	}
	fi, _ := os.Stat(path)
	p := struct {
		server.Forum
		DataBinSize uint64
		DataBinTime string
	}{}
	p.Forum = *common.Kforum
	if fi != nil {
		p.DataBinSize = uint64(fi.Size())
		p.DataBinTime = fi.ModTime().Format(time.RFC1123)
	}
	server.Render(w, server.TmplHelp, p)
}

// url: /robots.txt
func RobotsTxt(w http.ResponseWriter, r *http.Request) {
	serveFileFromDir(w, r, "static", "robots.txt")
}

func Cookie(w http.ResponseWriter, r *http.Request) {
	if m := r.FormValue("admin"); m == common.Kpassword {
		// admin requesting a cookie
		u, parts := server.User{}, strings.Split(r.FormValue("makeid"), ",")
		copy(u.ID[:], parts[0])
		u.M, _, _, _, _, _, _, _, _, _ = atoi(parts[1])
		if len(parts) > 2 {
			_, _, _, _, u.N, _, _, _, _, _ = atoi(parts[2])
		}
		common.Kforum.SetUser(w, u)
		http.Redirect(w, r, "/", 302)
		return
	}
	if m := r.FormValue("makeid"); m != "" {
		if !common.Kforum.GetUser(r).Can(server.PERM_ADMIN) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		u, parts := server.User{}, strings.Split(m, ",")
		copy(u.ID[:], parts[0])
		u.M, _, _, _, _, _, _, _, _, _ = atoi(parts[1])
		if len(parts) > 2 {
			_, _, _, _, u.N, _, _, _, _, _ = atoi(parts[2])
		}
		w.Write([]byte(common.Kforum.SetUser(nil, u)))
		return
	}
	if m := r.FormValue("uid"); m != "" {
		cookie := &http.Cookie{
			Name:    "uid",
			Value:   m,
			Path:    "/",
			Expires: time.Now().AddDate(1, 0, 0),
		}
		http.SetCookie(w, cookie)
		http.Redirect(w, r, "/", 302)
		return
	}

	w.Write([]byte("<html><title>Boon</title><form><input name='admin'/> <input name='makeid'/> <input type='submit'/></form></html>"))

	uid, _ := r.Cookie("uid")
	if uid != nil {
		w.Write([]byte("[uid]: " + uid.Value))
	}
}

func Mod(w http.ResponseWriter, r *http.Request) {
	if !common.Kforum.GetUser(r).Can(server.PERM_ADMIN) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m := &runtime.MemStats{}
	runtime.ReadMemStats(m)

	model := struct {
		server.Forum
		Errors  []*server.TimestampedMsg
		Notices []*server.TimestampedMsg
		Header  *http.Header
		IP      string
		IQLen   int
		runtime.MemStats
	}{
		Forum:    *common.Kforum,
		MemStats: *m,
		Errors:   common.Kforum.GetErrors(),
		Notices:  common.Kforum.GetNotices(),
		Header:   &r.Header,
		IQLen:    common.Kiq.Len(),
	}
	model.IP, _ = server.Format8Bytes(getIPAddress(r))
	server.Render(w, server.TmplLogs, model)
}
