package securestorage

import (
	"encoding/json"
	"fmt"
)

type DataStore struct {
	Store Store
}

func NewDataStore(store Store) *DataStore {
	return &DataStore{Store: store}
}

func (d *DataStore) ReadData() (Data, error) {
	if d == nil || d.Store == nil {
		return Data{}, fmt.Errorf("secure storage is not configured")
	}
	data, err := d.Store.Read()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return Data{}, nil
	}
	return data, nil
}

func (d *DataStore) Update(mut func(Data) error) error {
	data, err := d.ReadData()
	if err != nil {
		return err
	}
	if err := mut(data); err != nil {
		return err
	}
	if len(data) == 0 {
		return d.Store.Delete()
	}
	_, err = d.Store.Write(data)
	return err
}

func (d *DataStore) Get(key string, target any) (bool, error) {
	data, err := d.ReadData()
	if err != nil {
		return false, err
	}
	raw, ok := data[key]
	if !ok || raw == nil {
		return false, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return false, err
	}
	return true, nil
}

func (d *DataStore) Set(key string, value any) error {
	return d.Update(func(data Data) error {
		data[key] = value
		return nil
	})
}

func (d *DataStore) DeleteKey(key string) error {
	return d.Update(func(data Data) error {
		delete(data, key)
		return nil
	})
}
