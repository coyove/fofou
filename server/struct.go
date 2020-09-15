package server

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"strings"
	"time"

	"github.com/coyove/fofou/markup"
)

const (
	POST_ISDELETE = 1 << iota // used in archive only, normal deletion will have OP_DELETE
	POST_SHOWID
	POST_ISSAGE
)

const (
	POST_T_ISFIRST = 1 << iota
	POST_T_ISREF
	POST_T_ISNSFW // since NSFW is controlled by OP_NSFW and can be altered anytime, it is a transient status
	POST_T_ISYOU
	POST_T_ISOP
)

type Image struct {
	Path string
	Name string
	Size uint32
	X    uint16
	Y    uint16
}

type Post struct {
	Message   string
	Image     *Image
	user      [8]byte
	ip        [8]byte
	CreatedAt uint32
	ID        uint16
	Status    byte
	T_Status  byte
	Topic     *Topic
}

func (p *Post) T_SetStatus(v byte) { p.T_Status |= v }

func (p *Post) SetStatus(v byte) { p.Status |= v }

func (p *Post) T_UnsetStatus(v byte) { p.T_Status &= ^v }

func (p *Post) T_InvertStatus(v byte) { p.T_Status ^= v }

func (p *Post) InvertStatus(v byte) { p.Status ^= v }

func (p *Post) T_IsFirst() bool { return p.T_Status&POST_T_ISFIRST > 0 }

func (p *Post) T_IsRef() bool { return p.T_Status&POST_T_ISREF > 0 }

func (p *Post) T_IsNSFW() bool { return p.T_Status&POST_T_ISNSFW > 0 }

func (p *Post) T_IsOP() bool { return p.T_Status&POST_T_ISOP > 0 }

func (p *Post) T_IsYou() bool { return p.T_Status&POST_T_ISYOU > 0 }

func (p *Post) IsDeleted() bool { return p.Status&POST_ISDELETE > 0 }

func (p *Post) IsSaged() bool { return p.Status&POST_ISSAGE > 0 }

func (p *Post) Date() string {
	return time.Unix(int64(p.CreatedAt), 0).UTC().Add(8 * time.Hour).Format(stdTimeFormat)
}

func (p *Post) MessageHTML() string { return markup.Do(p.Message, true, 0) }

func (p *Post) aes128(a [8]byte) [8]byte {
	iv := [16]byte{}
	binary.BigEndian.PutUint32(iv[:], p.Topic.CreatedAt)
	binary.BigEndian.PutUint32(iv[4:], p.Topic.ID)
	copy(iv[8:], p.Topic.Subject)

	p.Topic.store.block.Encrypt(iv[:], iv[:])
	// the first 2 bytes of 'a' will never be encrypted
	for i := 2; i < 8; i++ {
		a[i] ^= iv[i]
	}
	return a
}

func (p *Post) IPXor() [8]byte { return p.aes128(p.ip) }

func (p *Post) UserXor() [8]byte { return p.aes128(p.user) }

func (p *Post) IP() string { i, _ := Format8Bytes(p.IPXor()); return i }

func (p *Post) User() string { _, i := Format8Bytes(p.UserXor()); return i }

func (p *Post) UserHTML() string {
	if p.user[0] == 0 {
		return ""
	}
	return "<span class='special-user'>" + p.User()[1:] + "</span>"
}

func (p *Post) LongID() uint64 {
	if p.ID >= 1<<12 || p.ID == 0 {
		panic("invalid post ID")
	}

	ti, pi := uint64(p.Topic.ID), uint64(p.ID-1)
	if pi < 4 {
		// ti(26) | 0 | ti(4) | 0 | ti(2) | pi(2)
		return ti>>6<<10 + (ti>>2&0xf)<<5 + (ti&0x3)<<2 + pi
	}
	if pi < 16 {
		// ti(28) | 0 | ti(4) | 1 | pi(4)
		return ti>>4<<10 + (ti&0xf)<<5 + 1<<4 + pi
	}
	if pi < 256 {
		// ti(32) | 1 | pi(4) | 0 | pi(4)
		return ti<<10 + 1<<9 + (pi>>4)<<5 + (pi & 0xf)
	}
	// ti(32) | pi(4) | 1 | pi(4) | 1 | pi(4)
	return ti<<14 + pi>>8<<10 + 1<<9 + (pi>>4&0xf)<<5 + 1<<4 + (pi & 0xf)
}

func SplitID(longid uint64) (uint32, uint16) {
	x, y := longid>>9&1, longid>>4&1
	if x == 0 && y == 0 {
		return uint32(longid>>10)<<6 + uint32(longid>>5&0xf)<<2 + uint32(longid>>2&0x3), uint16(longid&0x3) + 1
	}
	if x == 0 && y == 1 {
		return uint32(longid>>10)<<4 + uint32(longid>>5&0xf), uint16(longid&0xf) + 1
	}
	if x == 1 && y == 0 {
		return uint32(longid >> 10), uint16(longid>>5&0xf)<<4 + uint16(longid&0xf) + 1
	}
	return uint32(longid >> 14), uint16(longid>>10&0xf)<<8 + uint16(longid>>5&0xf)<<4 + uint16(longid&0xf) + 1
}

// Topic describes topic
type Topic struct {
	ID         uint32
	Sticky     bool
	Locked     bool
	Archived   bool
	FreeReply  bool
	Saged      bool
	CreatedAt  uint32
	ModifiedAt uint32
	Subject    string
	Next       *Topic
	Prev       *Topic
	Posts      []Post

	T_TotalPosts uint16
	T_IsAdmin    bool
	T_IsExpand   bool

	store *Store
}

func (p *Topic) Date() string { return time.Unix(int64(p.CreatedAt), 0).Format(stdTimeFormat) }

func (p *Topic) LastDate() string { return time.Unix(int64(p.ModifiedAt), 0).Format(stdTimeFormat) }

func (t *Topic) Reparent(you [8]byte) {
	op := t.Posts[0].user
	you = t.Posts[0].aes128(you)

	for i := 0; i < len(t.Posts); i++ {
		t.Posts[i].Topic = t
		x := t.Posts[i].user
		if x == op {
			t.Posts[i].T_SetStatus(POST_T_ISOP)
		}
		if x == you {
			t.Posts[i].T_SetStatus(POST_T_ISYOU)
		}
	}
}

// ForumConfig is a static configuration of a single forum
type ForumConfig struct {
	Invalidate     int64
	Title          string
	NoMoreNewUsers bool
	NoImageUpload  bool
	NoRecaptcha    bool
	MaxImageSize   int
	MaxSubjectLen  int
	MaxMessageLen  int
	MinMessageLen  int
	SearchTimeout  int
	Cooldown       int
	PostsPerPage   int
	TopicsPerPage  int
	URL            string
	Announcement   string

	// omit
	Salt            [16]byte `json:"-"`
	RecaptchaToken  string   `json:"-"`
	RecaptchaSecret string   `json:"-"`
}

func (config *ForumConfig) CorrectValues() {
	checkInt := func(i *int, v int) { *i = int(^(^uint32(*i-1)>>31)&1)*v + *i }
	checkInt(&config.MaxSubjectLen, 60)
	checkInt(&config.MaxMessageLen, 10000)
	checkInt(&config.MinMessageLen, 3)
	checkInt(&config.SearchTimeout, 100)
	checkInt(&config.MaxImageSize, 4)
	checkInt(&config.Cooldown, 2)
	checkInt(&config.PostsPerPage, 20)
	checkInt(&config.TopicsPerPage, 15)
}

func (config *ForumConfig) SetSalt(v string) [16]byte {
	v = strings.Repeat(v, 16) + "a-16chars-string"
	copy(config.Salt[:], v)
	return config.Salt
}

// Forum describes forum
type Forum struct {
	*ForumConfig
	*Store
	*Logger
}

const userStructSize = 8 + 4 + 4 + 8 + 8

const (
	PERM_ADMIN = 1 << iota
	PERM_NO_ROLL
	PERM_LOCK_SAGE_DELETE_FLAG
	PERM_STICKY_PURGE
	PERM_BLOCK
	PERM_APPEND_ANNOUNCE
)

type User struct {
	ID      [8]byte
	N       uint32
	Posts   uint32
	T       int64
	M       byte
	padding [7]byte
	Hash    string
}

func (u User) IsValid() bool { return u.ID != default8Bytes }

func (u User) Can(perm byte) bool { return u.M&perm > 0 }

func (u User) CanModerate() bool {
	return u.Can(PERM_ADMIN) || u.Can(PERM_LOCK_SAGE_DELETE_FLAG) || u.Can(PERM_STICKY_PURGE) || u.Can(PERM_BLOCK) || u.Can(PERM_APPEND_ANNOUNCE)
}

type SafeJSON struct {
	*bytes.Buffer
}

func (s *SafeJSON) Write(p []byte) (int, error) {
	for _, v := range p {
		if v == ',' {
			s.Buffer.WriteByte('^')
			continue
		}
		if v == '"' {
			s.Buffer.WriteByte('\'')
			continue
		}
		if bytes.IndexByte([]byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ[]{}:"), v) > -1 {
			s.Buffer.WriteByte(v)
			continue
		}
		if v == 0xa {
			continue
		}
		panic(v)
	}
	return len(p), nil
}

func (s *SafeJSON) Read(p []byte) (int, error) {
	n, err := s.Buffer.Read(p)
	for i, v := range p[:n] {
		if v == '^' {
			p[i] = ','
			continue
		}
		if v == '\'' {
			p[i] = '"'
			continue
		}
		if bytes.IndexByte([]byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ[]{}:"), v) > -1 {
			continue
		}
	}
	return n, err
}

type ResponseWriterWrapper struct {
	http.ResponseWriter
	Code        int
	ForceFooter bool
	Survey      interface{}
}

func (r *ResponseWriterWrapper) WriteHeader(code int) {
	r.Code = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *ResponseWriterWrapper) Write(p []byte) (int, error) {
	if r.Survey != nil {
	}
	return r.ResponseWriter.Write(p)
}
