package store

import (
	"reflect"
	. "github.com/seewindcn/GoStore"
	"github.com/seewindcn/GoStore/db"
	"github.com/seewindcn/GoStore/cache"
	_ "github.com/seewindcn/GoStore/db/mongo"
	_ "github.com/seewindcn/GoStore/cache/redis"
	"fmt"
	"log"
	"github.com/seewindcn/GoStore/lock"
	"time"
	"errors"
)


type Store struct {
	lockMgr *lock.LockMgr
	Cache cache.Cache
	StCache cache.StructCache
	SetCache cache.SetCache
	ListCache cache.ListCache
	Db db.DB
	Infos TableInfos
	ServiceAgent IServiceAgent
}


func New() *Store {
	s := &Store{
		Infos:make(map[reflect.Type]*TableInfo),
	}
	s.ServiceAgent = NewStoreServiceAgent(s)
	return s
}

func (self *Store) NewCache(name string) error {
	c, err := cache.NewCache(name)
	if err != nil {
		return err
	}
	self.Cache = c
	self.StCache = c.(cache.StructCache)
	self.SetCache = c.(cache.SetCache)
	self.ListCache = c.(cache.ListCache)
	return nil
}

func (self *Store) NewDB(name string) error {
	_db, err := db.NewDB(name)
	if err != nil {
		return err
	}
	self.Db = _db
	return nil
}

func (self *Store) NewLockMgr(name string, expiry time.Duration, tries int, delay time.Duration) {
	self.lockMgr = lock.New()
	self.lockMgr.Init(self, name, expiry, tries, delay)
}

func (self *Store) NewLock(name string) lock.Lock {
	return self.lockMgr.NewLock(name)
}

func (self *Store) NewLockEx(name string, expiry time.Duration, tries int, delay time.Duration) lock.Lock {
	return self.lockMgr.NewLockEx(name, expiry, tries, delay)
}

func (self *Store) Start(dbCfg M, cacheCfg M) error {
	if dbCfg != nil {
		if err := self.Db.Start(self.Infos, dbCfg); err != nil {
			return err
		}
	}
	if cacheCfg != nil {
		if err := self.Cache.Start(cacheCfg); err != nil {
			return err
		}
	}
	self.ServiceAgent.Start()
	return nil
}

// register table's struct
func (self *Store) RegTable(table string, st reflect.Type, isCache bool, index *DbIndex) {
	if st == nil {
		panic("store: RegTable st is nil")
	}
	if _, ok := self.Infos[st]; ok {
		panic("store: RegTable call twice for table " + table)
	}
	info := NewTableInfo()
	info.Name = table
	info.IsCache = isCache
	info.SType = st
	info.Index = index
	self.Infos[st] = info
	self.Db.RegTable(info)
}

// CacheObj cache obj's some fields
func (self *Store) CacheObj(obj interface{}) error {
	info := self.Infos.GetTableInfo(obj)
	if info == nil {
		panic(fmt.Sprintf("store save: info no found for obj:%s", obj))
	}
	return self.StCache.PutStruct(info.Name, info.GetStrKey(obj), obj, 0)
}

func (self *Store) Save(obj interface{}) error {
	info := self.Infos.GetTableInfo(obj)
	if info == nil {
		panic(fmt.Sprintf("store save: info no found for obj:%s", obj))
	}
	if err := self.Db.SaveByInfo(info, obj); err != nil {
		return err
	}
	if info.IsCache {
		self.StCache.PutStruct(info.Name, info.GetStrKey(obj), obj, 0)
	}
	return nil
}

// Load load from cache or db,
func (self *Store) Load(obj interface{}, cache bool) error {
	info := self.Infos.GetTableInfo(obj)
	if info == nil {
		panic(fmt.Sprintf("store save: info no found for obj:%s", obj))
	}
	if reflect.TypeOf(obj).Kind() != reflect.Ptr {
		panic("store load obj much be struct's pointer")
	}
	if cache && info.IsCache && self.StCache != nil {
		key := info.GetStrKey(obj)
		exist, err := self.StCache.GetStruct(info.Name, key, obj)
		log.Println("[store] load from cache", key, exist, err)
		if err == nil && exist {
			return nil
		}
	}
	return self.Db.LoadByInfo(info, obj)
}

//Loads objs: Ptr for []TableObject
func (self *Store) Loads(query M, objs interface{}, options *db.LoadOption) error {
	t := reflect.TypeOf(objs)
	v := t.Elem().Elem()
	if !(t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Slice && v.Kind() == reflect.Struct) {
		return errors.New("store loads objs much be []struct pointer")
	}

	info := self.Infos.GetTableInfo(v)
	self.Db.Loads(info.Name, query, objs, options)
	return nil
}

func (self *Store) CheckAndRegister(hash, name, value string) (string, bool) {
	val, err := self.StCache.GetStField(hash, "", name, reflect.String)
	if err != nil || val.(string) == ""  { // not exist
		if value == "" {  // if not exist, return "", false
			return "", false
		}

		// set value
		l := self.NewLock("_CAR_" + hash + "-" + name)
		l.Lock()
		defer l.Unlock()
		val, err = self.StCache.GetStField(hash, "", name, reflect.String)
		if (err != nil || val.(string) == "") {
			self.StCache.SetStField(hash, "", name, value, true)
			return value, true
		}
		return "", false
	}
	return val.(string), false
}

func (self *Store) UnRegister(hash, name, oldVal string) bool {
	l := self.NewLock("_CAR_" + hash + "-" + name)
	l.Lock()
	defer l.Unlock()
	val, err := self.StCache.GetStField(hash, "", name, reflect.String)
	if err == nil {
		if oldVal == "" || val.(string) == oldVal {
			self.StCache.DelStFields(hash, "", name)
			return true
		}
	}
	return false
}



