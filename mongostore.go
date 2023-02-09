package mongostore

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/qiniu/qmgo"
	"github.com/qiniu/qmgo/options"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongoOpts "go.mongodb.org/mongo-driver/mongo/options"
)

var (
	trueKey      = true
	ErrInvalidId = errors.New("mongo-store: invalid session id")
)

// Session object store in MongoDB
type Session struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	Data     string             `bson:"data"`
	Modified time.Time          `bson:"modified"`
}

// MongoStore stores sessions in MongoDB
type MongoStore struct {
	Codecs  []securecookie.Codec
	Options *sessions.Options
	Token   TokenGetSeter
	coll    *qmgo.Collection
}

// NewMongoStore returns a new MongoStore.
// Set ensureTTL to true let the database auto-remove expired object by maxAge.
func NewMongoStore(c *qmgo.Collection, maxAge int, ensureTTL bool,
	keyPairs ...[]byte) *MongoStore {
	store := &MongoStore{
		Codecs: securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: maxAge,
		},
		Token: &CookieToken{},
		coll:  c,
	}

	store.MaxAge(maxAge)

	if ensureTTL {
		exp := int32(time.Duration(maxAge) * time.Second)
		indexKey := []options.IndexModel{
			{Key: []string{"modified"}, IndexOptions: &mongoOpts.IndexOptions{
				ExpireAfterSeconds: &exp,
				Sparse:             &trueKey,
				Unique:             &trueKey,
			}},
		}
		err := c.CreateIndexes(context.Background(), indexKey)
		if err != nil {
			return nil
		}
	}

	return store
}

// Get registers and returns a session for the given name and session store.
// It returns a new session if there are no sessions registered for the name.
func (m *MongoStore) Get(r *http.Request, name string) (
	*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(m, name)
}

// New returns a session for the given name without adding it to the registry.
func (m *MongoStore) New(r *http.Request, name string) (
	*sessions.Session, error) {
	session := sessions.NewSession(m, name)
	session.Options = &sessions.Options{
		Path:     m.Options.Path,
		MaxAge:   m.Options.MaxAge,
		Domain:   m.Options.Domain,
		Secure:   m.Options.Secure,
		HttpOnly: m.Options.HttpOnly,
	}
	session.IsNew = true
	var err error
	if cook, errToken := m.Token.GetToken(r, name); errToken == nil {
		err = securecookie.DecodeMulti(name, cook, &session.ID, m.Codecs...)
		if err == nil {
			err = m.load(session)
			if err == nil {
				session.IsNew = false
			} else {
				err = nil
			}
		}
	}
	return session, err
}

// Save saves all sessions registered for the current request.
func (m *MongoStore) Save(r *http.Request, w http.ResponseWriter,
	session *sessions.Session) error {
	if session.Options.MaxAge < 0 {
		if err := m.delete(session); err != nil {
			return err
		}
		m.Token.SetToken(w, session.Name(), "", session.Options)
		return nil
	}

	if session.ID == "" {
		session.ID = primitive.NewObjectID().Hex()
	}

	if err := m.upsert(session); err != nil {
		return err
	}

	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID,
		m.Codecs...)
	if err != nil {
		return err
	}

	m.Token.SetToken(w, session.Name(), encoded, session.Options)
	return nil
}

// MaxAge sets the maximum age for the store and the underlying cookie
// implementation. Individual sessions can be deleted by setting Options.MaxAge
// = -1 for that session.
func (m *MongoStore) MaxAge(age int) {
	m.Options.MaxAge = age

	// Set the maxAge for each securecookie instance.
	for _, codec := range m.Codecs {
		if sc, ok := codec.(*securecookie.SecureCookie); ok {
			sc.MaxAge(age)
		}
	}
}

func (m *MongoStore) load(session *sessions.Session) error {
	if !primitive.IsValidObjectID(session.ID) {
		return ErrInvalidId
	}

	oID, err := primitive.ObjectIDFromHex(session.ID)
	if err != nil {
		return err
	}

	s := Session{}
	err = m.coll.Find(context.Background(), bson.M{"_id": oID}).One(&s)
	if err != nil {
		return err
	}

	if err := securecookie.DecodeMulti(session.Name(), s.Data, &session.Values,
		m.Codecs...); err != nil {
		return err
	}

	return nil
}

func (m *MongoStore) upsert(session *sessions.Session) error {
	if !primitive.IsValidObjectID(session.ID) {
		return ErrInvalidId
	}

	var modified time.Time
	if val, ok := session.Values["modified"]; ok {
		modified, ok = val.(time.Time)
		if !ok {
			return errors.New("mongo-store: invalid modified value")
		}
	} else {
		modified = time.Now()
	}

	encoded, err := securecookie.EncodeMulti(session.Name(), session.Values,
		m.Codecs...)
	if err != nil {
		return err
	}

	oID, err := primitive.ObjectIDFromHex(session.ID)
	if err != nil {
		return err
	}

	s := Session{
		ID:       oID,
		Data:     encoded,
		Modified: modified,
	}

	_, err = m.coll.UpsertId(context.Background(), s.ID, &s)
	if err != nil {
		return err
	}

	return nil
}

func (m *MongoStore) delete(session *sessions.Session) error {
	if !primitive.IsValidObjectID(session.ID) {
		return ErrInvalidId
	}

	oID, err := primitive.ObjectIDFromHex(session.ID)
	if err != nil {
		return err
	}

	return m.coll.RemoveId(context.Background(), oID)
}
