package database

import (
	"time"

	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

type WorkerInfo struct {
	ID         string    `db:"id"`
	UpdateTime time.Time `db:"updated_at"`
	Ip         string    `db:"ip"`
	SvcUri     string    `db:"svc_uri"`  // download service, it should be http://ip:port
	SvcConn    int       `db:"svc_conn"` // number of downloading connections
	Online     bool      `db:"online"`
	Disable    bool      `db:"disable"`
}

func GetWorkerInfo(id string) (*WorkerInfo, error) {
	db := GetDB()
	info := &WorkerInfo{}
	if err := database.QueryStruct(db, info, "SELECT * FROM worker_info WHERE id=?", id); err != nil {
		return nil, errors.As(err)
	}

	return info, nil
}

func SearchWorkerInfo(ip string) ([]WorkerInfo, error) {
	db := GetDB()
	infos := []WorkerInfo{}
	if err := database.QueryStructs(db, &infos, "SELECT * FROM worker_info WHERE ip=?", ip); err != nil {
		return nil, errors.As(err)
	}
	return infos, nil
}

func AllWorkerInfo() ([]WorkerInfo, error) {
	db := GetDB()
	infos := []WorkerInfo{}
	if err := database.QueryStructs(db, &infos, "SELECT * FROM worker_info"); err != nil {
		return nil, errors.As(err)
	}
	return infos, nil
}

func OnlineWorker(info *WorkerInfo) error {
	db := GetDB()
	exist := 0
	if err := database.QueryElem(db, &exist, "SELECT count(*) FROM worker_info WHERE id=?", info.ID); err != nil {
		return errors.As(err, *info)
	}
	if exist == 0 {
		if _, err := database.InsertStruct(db, info, "worker_info"); err != nil {
			return errors.As(err, *info)
		}
		return nil
	}
	if _, err := db.Exec(
		"UPDATE worker_info SET ip=?, svc_uri=?, svc_conn=0, online=1, updated_at=? WHERE id=?",
		info.Ip, info.SvcUri, info.UpdateTime, info.ID,
	); err != nil {
		return errors.As(err, *info)
	}
	return nil
}

func OfflineWorker(id string) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE worker_info SET online=0,updated_at=? WHERE id=?", time.Now(), id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

func DisableWorker(id string, disable bool) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE worker_info SET disable=?,updated_at=? WHERE id=?", disable, time.Now(), id); err != nil {
		return errors.As(err, id, disable)
	}
	return nil
}

func AddWorkerConn(id string, num int) error {
	db := GetDB()
	if _, err := db.Exec("UPDATE worker_info SET svc_conn=svc_conn+? WHERE id=?", num, id); err != nil {
		return errors.As(err, id)
	}
	return nil
}

func FindWorker(svc_uri, id string) ([]WorkerInfo, error) {
	db := GetDB()
	infos := []WorkerInfo{}
	if err := database.QueryStructs(db, &infos, "SELECT * FROM worker_info WHERE svc_uri=? and id != ?", svc_uri, id); err != nil {
		return nil, errors.As(err)
	}
	return infos, nil
}

func DeleteWorker(id string) error {
	db := GetDB()
	if _, err := db.Exec("DELETE FROM worker_info WHERE id=?", id); err != nil {
		return errors.As(err, id)
	}
	return nil
}
