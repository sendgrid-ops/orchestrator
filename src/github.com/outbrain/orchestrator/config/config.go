/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package config

import (
    "encoding/json"
    "os"
    
	"github.com/outbrain/log"
)

// Configuration makes for orchestrator configuration input, which can be provided by user via JSON formatted file.
// Some of the parameteres have reasonable default values, and some (like database credentials) are 
// strictly expected from user.
type Configuration struct {
	MySQLTopologyUser		string
	MySQLTopologyPassword	string
	MySQLOrchestratorHost	string
	MySQLOrchestratorPort	uint
	MySQLOrchestratorDatabase	string
	MySQLOrchestratorUser		string
	MySQLOrchestratorPassword	string
	SlaveLagQuery				string		// custom query to check on slave lg (e.g. heartbeat table)
	SlaveStartPostWaitMilliseconds	int		// Time to wait after START SLAVE before re-readong instance (give slave chance to connect to master)
	DiscoverByShowSlaveHosts	bool		// Attempt SHOW SLAVE HOSTS before PROCESSLIST
	InstancePollSeconds			uint		// Number of seconds between instance reads
	UnseenInstanceForgetHours	uint		// Number of hours after which an unseen instance is forgotten
	DiscoveryPollSeconds		int			// Auto/continuous discovery of instances sleep time between polls
	ReasonableReplicationLagSeconds	int		// Abvoe this value is considered a problem
	ReasonableMaintenanceReplicationLagSeconds int // Above this value move-up and move-below are blocked
	AuditPageSize		int
	HTTPAuthUser		string				// Username for HTTP Basic authentication (blank disables authentication)
	HTTPAuthPassword	string				// Password for HTTP Basic authentication
}	

var Config *Configuration = NewConfiguration()

func NewConfiguration() *Configuration {
	return &Configuration {
		InstancePollSeconds:		60,
		UnseenInstanceForgetHours:	240,
		SlaveStartPostWaitMilliseconds: 1000,
		DiscoverByShowSlaveHosts:	false,
		DiscoveryPollSeconds:		5,
		ReasonableReplicationLagSeconds: 10,
		ReasonableMaintenanceReplicationLagSeconds: 20,
		AuditPageSize:				20,
		HTTPAuthUser: 				"",
		HTTPAuthPassword: 			"",
	}
}


// read reads configuration from given file, or silently skips if the file does not exist.
// If the file does exist, then it is expected to be in valid JSON format or the function bails out.
func read(file_name string) (*Configuration, error) {
	file, err := os.Open(file_name)
	if err == nil {
		decoder := json.NewDecoder(file)
		err := decoder.Decode(Config)
		if err == nil {
			log.Infof("Read config: %s", file_name)
		} else {
	  		log.Fatal("Cannot read config file:", file_name, err)
		}
	}
	return Config, err
}


// Read reads configuration from zero, either, some or all given files, in order of input.
// A file can override configuration provided in previous file.
func Read(file_names ...string) *Configuration {
	for _, file_name := range file_names {
		read(file_name)
	}
	return Config
}

// ForceRead reads configuration from given file name or bails out if it fails
func ForceRead(file_name string) *Configuration {
	_, err := read(file_name)
	if err != nil {
		log.Fatal("Cannot read config file:", file_name, err)
	}
	return Config
}
