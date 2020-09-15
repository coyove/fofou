package common

import (
	"strings"
	"time"

	"github.com/coyove/common/lru"
	"github.com/coyove/fofou/server"
)

const (
	DATA_IMAGES    = "data/images/"
	DATA_LOGS      = "data/logs/"
	DATA_MAIN      = "data/main.txt"
	DATA_RECAPTCHA = "data/recaptcha.txt"
)

var (
	Kforum     *server.Forum
	Kiq        *server.ImageQueue
	KthrotIPID *lru.Cache
	KbadUsers  *lru.Cache
	Kuuids     *lru.Cache
	Karchive   *lru.Cache
	Kprod      bool
	Kpassword  string
	Kstart     time.Time
)

var TopicFilter1 = func(t *server.Topic) bool { return !strings.HasPrefix(t.Subject, "!!") }
var TopicFilter2 = func(t *server.Topic) bool { return strings.HasPrefix(t.Subject, "!!") }
