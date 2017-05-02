// Copyright 2017 Factom Foundation
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package state

import (
	"fmt"
	"io/ioutil"
	"sync"
)

var tmpState []byte
var mutex sync.Mutex
var stop bool

//To be increased whenever the data being saved changes from the last verion
const version = 4

func StopSaving() {
	mutex.Lock()
	defer mutex.Unlock()
	stop = true
}

func SaveDBStateList(ss *DBStateList, networkName string, fileLocation string) error {
	//For now, to file. Later - to DB
	if stop == true {
		return nil
	}
	mutex.Lock()
	defer mutex.Unlock()

	//Don't save States after the server has booted - it might start it in a wrong state
	if ss.State.DBFinished == true {
		return nil
	}

	//Save only every 1000 states
	if ss.GetHighestSavedBlk()%1000 != 0 || ss.GetHighestSavedBlk() < 1000 {
		return nil
	}

	//Actually save data from previous cached state to prevent dealing with rollbacks
	if len(tmpState) > 0 {
		err := SaveToFile(tmpState, NetworkIDToFilename(networkName, fileLocation))
		if err != nil {
			return err
		}
	}

	//Marshal state for future saving
	b, err := ss.MarshalBinary()
	if err != nil {
		return err
	}
	tmpState = b

	return nil
}

func LoadDBStateList(ss *DBStateList, networkName string, fileLocation string) error {
	b, err := LoadFromFile(NetworkIDToFilename(networkName, fileLocation))
	if err != nil {
		return nil
	}
	if b == nil {
		return nil
	}

	return ss.UnmarshalBinary(b)
}

/*
func SaveTheState(ss *SaveState, networkName string) error {
	//For now, to file. Later - to DB

	//Save only every 1000 states
	if ss.DBHeight%1000 != 0 || ss.DBHeight < 1000 {
		return nil
	}

	//Actually save data from previous cached state to prevent dealing with rollbacks
	if len(tmpState) > 0 {
		err := SaveToFile(tmpState, NetworkIDToFilename(networkName))
		if err != nil {
			return err
		}
	}

	//Marshal state for future saving
	b, err := ss.MarshalBinary()
	if err != nil {
		return err
	}
	tmpState = b

	return nil
}
*/

func NetworkIDToFilename(networkName string, fileLocation string) string {
	file := fmt.Sprintf("FastBoot_%s_v%v.db", networkName, version)
	if fileLocation != "" {
		return fmt.Sprintf("%v/%v", fileLocation, file)
	}
	return file
}

/*
func LoadState(ss *SaveState, networkName string) error {
	b, err := LoadFromFile(NetworkIDToFilename(networkName))
	if err != nil {
		return nil
	}
	if b == nil {
		return nil
	}

	return ss.UnmarshalBinary(b)
}
*/

func SaveToFile(b []byte, filename string) error {
	err := ioutil.WriteFile(filename, b, 0644)
	if err != nil {
		return err
	}
	return nil
}

func LoadFromFile(filename string) ([]byte, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return b, nil
}
