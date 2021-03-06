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

package inst

import (
	"fmt"
	"errors"
	"time"
	"strings"
	"database/sql"
	"github.com/outbrain/sqlutils"
	"github.com/outbrain/orchestrator/db"
	"github.com/outbrain/orchestrator/config"
	"github.com/outbrain/log"
)

// ExecInstance executes a given query on the given MySQL topology instance
func ExecInstance(instanceKey *InstanceKey, query string, args ...interface{}) (sql.Result, error) {
	db,	err	:=	db.OpenTopology(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return nil, err
	}
	res, err := sqlutils.Exec(db, query, args...)
	return res, err
}

// ScanInstanceRow executes a read-a-single-row query on a given MySQL topology instance 
func ScanInstanceRow(instanceKey *InstanceKey, query string, dest ...interface{}) error {
	db,	err	:=	db.OpenTopology(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return err
	}
	err = db.QueryRow(query).Scan(dest...)
	return err
}


// ReadTopologyInstance connects to a topology MySQL instance and reads its configuration and 
// replication status. It writes read info into orchestrator's backend.
func ReadTopologyInstance(instanceKey *InstanceKey) (*Instance, error) {
    defer func() {
        if err := recover(); err != nil {
            log.Errorf("Unexpected error: %+v", err)
        }
    }()

	instance := NewInstance()
	instanceFound := false;
    foundBySlaveHosts := false


	db,	err	:=	db.OpenTopology(instanceKey.Hostname, instanceKey.Port)

    if err != nil {goto Cleanup}

   	instance.Key = *instanceKey
    err = db.QueryRow("select @@global.server_id, @@global.version, @@global.binlog_format, @@global.log_bin, @@global.log_slave_updates").Scan(
       	&instance.ServerID, &instance.Version, &instance.Binlog_format, &instance.LogBinEnabled, &instance.LogSlaveUpdatesEnabled)
    if err != nil {goto Cleanup}
    instanceFound = true
    err = sqlutils.QueryRowsMap(db, "show slave status", func(m sqlutils.RowMap) error {
		instance.Slave_IO_Running = (m.GetString("Slave_IO_Running") == "Yes")
      	instance.Slave_SQL_Running = (m.GetString("Slave_SQL_Running") == "Yes")
       	instance.ReadBinlogCoordinates.LogFile = m.GetString("Master_Log_File")
       	instance.ReadBinlogCoordinates.LogPos = m.GetInt64("Read_Master_Log_Pos")
       	instance.ExecBinlogCoordinates.LogFile = m.GetString("Relay_Master_Log_File")
       	instance.ExecBinlogCoordinates.LogPos = m.GetInt64("Exec_Master_Log_Pos")

       	masterKey, err := NewInstanceKeyFromStrings(m.GetString("Master_Host"), m.GetString("Master_Port")) 
       	if err != nil {log.Errore(err)}
       	instance.MasterKey = *masterKey
   		instance.SecondsBehindMaster = m.GetNullInt64("Seconds_Behind_Master")
       	if config.Config.SlaveLagQuery == "" {
       		instance.SlaveLagSeconds = instance.SecondsBehindMaster
        }
        // Not breaking the flow even on error
       	return nil
   	})
    if err != nil {goto Cleanup}

	if config.Config.SlaveLagQuery != "" {
		err = db.QueryRow(config.Config.SlaveLagQuery).Scan(&instance.SlaveLagSeconds)
	    if err != nil {goto Cleanup}
	}
        
    err = sqlutils.QueryRowsMap(db, "show master status", func(m sqlutils.RowMap) error {
    	var err error
       	instance.SelfBinlogCoordinates.LogFile = m.GetString("File")
       	instance.SelfBinlogCoordinates.LogPos = m.GetInt64("Position")
       	return err
   	})
    if err != nil {goto Cleanup}
        
    // Get slaves, either by SHOW SLAVE HOSTS or via PROCESSLIST
    if config.Config.DiscoverByShowSlaveHosts {
        err = sqlutils.QueryRowsMap(db, `show slave hosts`, 
        		func(m sqlutils.RowMap) error {
        			slaveKey, err := NewInstanceKeyFromStrings(m.GetString("Host"), m.GetString("Port")) 
        			if err == nil {
						instance.AddSlaveKey(slaveKey)
						foundBySlaveHosts = true
					}
					return err
		       	})
		
        if err != nil {goto Cleanup}
    } 
    if !foundBySlaveHosts {
    	// Either not configured to read SHOW SLAVE HOSTS or nothing was there.
       	// Discover by processlist
        err = sqlutils.QueryRowsMap(db, `
        	select 
        		substring_index(host, ':', 1) as slave_hostname 
        	from 
        		information_schema.processlist 
        	where 
        		command='Binlog Dump'`, 
        		func(m sqlutils.RowMap) error {
        			cname, err := GetCNAME(m.GetString("slave_hostname"))
        			if err != nil {return err}
        			slaveKey := InstanceKey{Hostname: cname, Port: instance.Key.Port}
					instance.AddSlaveKey(&slaveKey)
					return err
		       	})
			
        if err != nil {goto Cleanup}
	}
    if err != nil {goto Cleanup}

	instance.ClusterName, err = ReadClusterNameByMaster(&instance.Key, &instance.MasterKey)
    if err != nil {goto Cleanup}

	Cleanup:
	if instanceFound {
		_ = WriteInstance(instance, err)
	} else {
		_ = UpdateInstanceLastChecked(instanceKey)
	}
	if err	!=	nil	{
		log.Errore(err)
	}
	return instance, err
}


// ReadClusterNameByMaster will return the cluster name for a given instance by looking at its master
// and getting it from there.
// It is a non-recursive function and so-called-recursion is performed upon periodic reading of 
// instances.
func ReadClusterNameByMaster(instanceKey *InstanceKey, masterKey *InstanceKey) (string, error) {
	db,	err	:=	db.OpenOrchestrator()
	if	err	!=	nil	{
		return "", log.Errore(err)
	}

	var clusterName string
    err = db.QueryRow(`
       	select 
       		if (
       			cluster_name != '',
       			cluster_name,
	       		ifnull(concat(max(hostname), ':', max(port)), '')
	       	) as cluster_name
       	from database_instance 
		 	where hostname=? and port=?`, 
		masterKey.Hostname, masterKey.Port).Scan(
		 	&clusterName,
		)
    if err != nil {return "", log.Errore(err)}
    if clusterName == "" {
    	return fmt.Sprintf("%s:%d", instanceKey.Hostname, instanceKey.Port), nil
    }
  	return clusterName, err
}


// ReadInstance reads an instance from the orchestrator backend database
func ReadInstance(instanceKey *InstanceKey) (*Instance, bool, error) {
	db,	err	:=	db.OpenOrchestrator()
	if	err	!=	nil	{
		return nil, false, log.Errore(err)
	}
	instance := NewInstance()
	instance.Key = *instanceKey

	var slaveHostsJson string
	var secondsSinceLastChecked uint

    err = db.QueryRow(`
       	select 
       		server_id,
			version,
			binlog_format,
			log_bin, 
			log_slave_updates,
			binary_log_file,
			binary_log_pos,
			master_host,
			master_port,
			slave_sql_running,
			slave_io_running,
			master_log_file,
			read_master_log_pos,
			relay_master_log_file,
			exec_master_log_pos,
			seconds_behind_master,
			slave_lag_seconds,
			slave_hosts,
			cluster_name,
			timestampdiff(second, last_checked, now()) as seconds_since_last_checked,
			(last_checked <= last_seen) is true as is_last_check_valid,
			timestampdiff(second, last_seen, now()) as seconds_since_last_seen
		 from database_instance 
		 	where hostname=? and port=?`, 
		 instanceKey.Hostname, instanceKey.Port).Scan(
		 	&instance.ServerID,
		 	&instance.Version,
		 	&instance.Binlog_format,
		 	&instance.LogBinEnabled,
		 	&instance.LogSlaveUpdatesEnabled,
		 	&instance.SelfBinlogCoordinates.LogFile,
		 	&instance.SelfBinlogCoordinates.LogPos,
		 	&instance.MasterKey.Hostname,
		 	&instance.MasterKey.Port,
		 	&instance.Slave_SQL_Running,
		 	&instance.Slave_IO_Running,
		 	&instance.ReadBinlogCoordinates.LogFile,
		 	&instance.ReadBinlogCoordinates.LogPos,
		 	&instance.ExecBinlogCoordinates.LogFile,
		 	&instance.ExecBinlogCoordinates.LogPos,
		 	&instance.SecondsBehindMaster,
		 	&instance.SlaveLagSeconds,
		 	&slaveHostsJson,
		 	&instance.ClusterName,
		 	&secondsSinceLastChecked,
		 	&instance.IsLastCheckValid,
		 	&instance.SecondsSinceLastSeen,
		)
	if err == sql.ErrNoRows {log.Infof("No entry for %+v", instanceKey); return instance, false, err}	
				
    if err != nil {log.Error("error on", instanceKey, err); return instance, false, err}
	instance.IsUpToDate = (secondsSinceLastChecked <= config.Config.InstancePollSeconds) 
	instance.IsRecentlyChecked = (secondsSinceLastChecked <= config.Config.InstancePollSeconds * 5) 
    instance.ReadSlaveHostsFromJson(slaveHostsJson)
    
	return instance, true, err
}


// readInstanceRow reads a single instance row from the orchestrator backend database.
func readInstanceRow(m sqlutils.RowMap) *Instance {
    instance := NewInstance()
    	
    instance.Key.Hostname = m.GetString("hostname")
    instance.Key.Port = m.GetInt("port")
	instance.Slave_IO_Running = (m.GetString("Slave_IO_Running") == "Yes")
   	instance.Slave_SQL_Running = (m.GetString("Slave_SQL_Running") == "Yes")
   	instance.ReadBinlogCoordinates.LogFile = m.GetString("Master_Log_File")
   	instance.ReadBinlogCoordinates.LogPos = m.GetInt64("Read_Master_Log_Pos")
   	instance.ExecBinlogCoordinates.LogFile = m.GetString("Relay_Master_Log_File")
    instance.ExecBinlogCoordinates.LogPos = m.GetInt64("Exec_Master_Log_Pos")
    instance.ServerID = m.GetUint("server_id")
 	instance.Version = m.GetString("version")
 	instance.Binlog_format = m.GetString("binlog_format")
 	instance.LogBinEnabled = m.GetBool("log_bin")
 	instance.LogSlaveUpdatesEnabled = m.GetBool("log_slave_updates")
 	instance.SelfBinlogCoordinates.LogFile = m.GetString("binary_log_file")
 	instance.SelfBinlogCoordinates.LogPos = m.GetInt64("binary_log_pos")
 	instance.MasterKey.Hostname = m.GetString("master_host")
 	instance.MasterKey.Port = m.GetInt("master_port")
 	instance.Slave_SQL_Running = m.GetBool("slave_sql_running")
 	instance.Slave_IO_Running = m.GetBool("slave_io_running")
 	instance.ReadBinlogCoordinates.LogFile = m.GetString("master_log_file")
 	instance.ReadBinlogCoordinates.LogPos = m.GetInt64("read_master_log_pos")
 	instance.ExecBinlogCoordinates.LogFile = m.GetString("relay_master_log_file")
 	instance.ExecBinlogCoordinates.LogPos = m.GetInt64("exec_master_log_pos")
 	instance.SecondsBehindMaster = m.GetNullInt64("seconds_behind_master")
 	instance.SlaveLagSeconds = m.GetNullInt64("slave_lag_seconds")
 	slaveHostsJson := m.GetString("slave_hosts")
 	instance.ClusterName = m.GetString("cluster_name")
 	instance.IsUpToDate = (m.GetUint("seconds_since_last_checked") <= config.Config.InstancePollSeconds) 
	instance.IsRecentlyChecked = (m.GetUint("seconds_since_last_checked") <= config.Config.InstancePollSeconds * 5) 
 	instance.IsLastCheckValid = m.GetBool("is_last_check_valid")
 	instance.SecondsSinceLastSeen = m.GetNullInt64("seconds_since_last_seen")
 	
 	instance.ReadSlaveHostsFromJson(slaveHostsJson)
 	return instance
}


// ReadClusterInstances reads all instances of a given cluster
func ReadClusterInstances(clusterName string) ([](*Instance), error) {
	instances := [](*Instance){}

	db,	err	:=	db.OpenOrchestrator()
	if	err	!=	nil	{
		return instances, log.Errore(err)
	}
	if strings.Index(clusterName, "'") >= 0 {
		return instances, log.Errorf("Invalid cluster name: %s", clusterName)	
	}

	query := fmt.Sprintf(`
		select 
			*,
			timestampdiff(second, last_checked, now()) as seconds_since_last_checked,
			(last_checked <= last_seen) is true as is_last_check_valid,
			timestampdiff(second, last_seen, now()) as seconds_since_last_seen
		from 
			database_instance 
		where
			cluster_name = '%s'
		order by
			hostname, port`, clusterName)

    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
		instance := readInstanceRow(m)
    	instances = append(instances, instance)
    	return nil       	
   	})

	return instances, err
}


// ReadProblemInstances reads all instances with problems
func ReadProblemInstances() ([](*Instance), error) {
	instances := [](*Instance){}

	db,	err	:=	db.OpenOrchestrator()
	if	err	!=	nil	{
		return instances, log.Errore(err)
	}

	query := fmt.Sprintf(`
		select 
			*,
			timestampdiff(second, last_checked, now()) as seconds_since_last_checked,
			(last_checked <= last_seen) is true as is_last_check_valid,
			timestampdiff(second, last_seen, now()) as seconds_since_last_seen
		from 
			database_instance 
		where
			(last_seen < last_checked)
			or (not ifnull(timestampdiff(second, last_checked, now()) <= %d, false))
			or (not slave_sql_running)
			or (not slave_io_running)
			or (seconds_behind_master > 10)
		order by
			hostname, port`, config.Config.InstancePollSeconds)

    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
		instance := readInstanceRow(m)
    	instances = append(instances, instance)
    	return nil       	
   	})

	return instances, err
}


// SearchInstances reads all instances qualifying for some searchString
func SearchInstances(searchString string) ([](*Instance), error) {
	instances := [](*Instance){}

	db,	err	:=	db.OpenOrchestrator()
	if	err	!=	nil	{
		return instances, log.Errore(err)
	}
	if strings.Index(searchString, "'") >= 0 {
		return instances, log.Errorf("Invalid searchString: %s", searchString)	
	}

	query := fmt.Sprintf(`
		select 
			*,
			timestampdiff(second, last_checked, now()) as seconds_since_last_checked,
			(last_checked <= last_seen) is true as is_last_check_valid,
			timestampdiff(second, last_seen, now()) as seconds_since_last_seen
		from 
			database_instance 
		where
			hostname like '%%%s%%'
			or cluster_name like '%%%s%%'
			or server_id = '%s'
			or version like '%%%s%%'
			or port = '%s'
			or concat(hostname, ':', port) like '%%%s%%'
		order by
			cluster_name,
			hostname, port`, searchString, searchString, searchString, searchString, searchString, searchString)
    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
		instance := readInstanceRow(m)
    	instances = append(instances, instance)
    	return nil       	
   	})

	return instances, err
}


// ReadClusters reads names of all known clusters
func ReadClusters() ([]string, error) {
	clusterNames := []string{}

	db,	err	:=	db.OpenOrchestrator()
	if	err	!=	nil	{
		return clusterNames, log.Errore(err)
	}

	query := fmt.Sprintf(`
		select 
			cluster_name
		from 
			database_instance 
		group by
			cluster_name`)

    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
    	clusterNames = append(clusterNames, m.GetString("cluster_name"))
    	return nil       	
   	})

	return clusterNames, err
}


// ReadOutdatedInstanceKeys reads and returns keys for all instances that are not up to date (i.e.
// pre-configured time has passed since they were last cheked)
func ReadOutdatedInstanceKeys() ([]InstanceKey, error) {
	res := []InstanceKey{}
	query := fmt.Sprintf(`
		select 
			hostname, port 
		from 
			database_instance 
		where
			last_checked < now() - interval %d second`, 
    		config.Config.InstancePollSeconds)
	db,	err	:=	db.OpenOrchestrator()
    if err != nil {goto Cleanup}
    
    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
    	instanceKey, merr := NewInstanceKeyFromStrings(m.GetString("hostname"), m.GetString("port"))
	    if merr != nil {
	    	log.Errore(merr)
	    } else {
	    	res = append(res, *instanceKey)
	    }
	    // We don;t return an error because we want to keep filling the outdated instances list.
    	return nil       	
   	})
	Cleanup:

	if err	!=	nil	{
		log.Errore(err)
	}
	return res, err

}


// WriteInstance stores an instance in the orchestrator backend
func WriteInstance(instance *Instance, lastError error) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
        	replace into database_instance (
        		hostname,
        		port,
        		last_checked,
        		server_id,
				version,
				binlog_format,
				log_bin,
				log_slave_updates,
				binary_log_file,
				binary_log_pos,
				master_host,
				master_port,
				slave_sql_running,
				slave_io_running,
				master_log_file,
				read_master_log_pos,
				relay_master_log_file,
				exec_master_log_pos,
				seconds_behind_master,
				slave_lag_seconds,
				num_slave_hosts,
				slave_hosts,
				cluster_name
			) values (?, ?, NOW(), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			instance.Key.Hostname, 
		 	instance.Key.Port,
		 	instance.ServerID,
		 	instance.Version,
		 	instance.Binlog_format,
		 	instance.LogBinEnabled,
		 	instance.LogSlaveUpdatesEnabled,
			instance.SelfBinlogCoordinates.LogFile,
			instance.SelfBinlogCoordinates.LogPos,
		 	instance.MasterKey.Hostname,
		 	instance.MasterKey.Port,
		 	instance.Slave_SQL_Running,
		 	instance.Slave_IO_Running,
		 	instance.ReadBinlogCoordinates.LogFile,
		 	instance.ReadBinlogCoordinates.LogPos,
		 	instance.ExecBinlogCoordinates.LogFile,
		 	instance.ExecBinlogCoordinates.LogPos,
		 	instance.SecondsBehindMaster,
		 	instance.SlaveLagSeconds,
		 	len(instance.SlaveHosts),
		 	instance.GetSlaveHostsAsJson(),
		 	instance.ClusterName,
		 	)
    if err != nil {return log.Errore(err)}
	
	if lastError == nil {
		sqlutils.Exec(db, `
        	update database_instance set last_seen = NOW() where hostname=? and port=?
        	`, instance.Key.Hostname, instance.Key.Port,
        )
	}
	return nil
}


// UpdateInstanceLastChecked updates the last_check timestamp in the orchestrator backed database 
// for a given instance
func UpdateInstanceLastChecked(instanceKey *InstanceKey) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
        	update 
        		database_instance 
        	set
        		last_checked = NOW()
			where 
				hostname = ?
				and port = ?`,
			instanceKey.Hostname, 
		 	instanceKey.Port,
		 	)
    if err != nil {return log.Errore(err)}

	return nil
}


// ForgetInstance removes an instance entry from the orchestrator backed database.
// It may be auto-rediscovered through topology or requested for discovery by multiple means.
func ForgetInstance(instanceKey *InstanceKey) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
			delete 
				from database_instance 
			where 
				hostname = ? and port = ?`,
			instanceKey.Hostname, 
		 	instanceKey.Port,
		 )
	AuditOperation("forget", instanceKey, "")
	return err		 
}


// ForgetLongUnseenInstances will remove entries of all instacnes that have long since been last seen.
func ForgetLongUnseenInstances() error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
			delete 
				from database_instance 
			where 
				last_seen < NOW() - interval ? hour`,
			config.Config.UnseenInstanceForgetHours,
		 )
	AuditOperation("forget-unseen", nil, "")
	return err		 
}


// RefreshTopologyInstance will synchronuously re-read topology instance
func RefreshTopologyInstance(instanceKey *InstanceKey) (*Instance, error) {
	_, err := ReadTopologyInstance(instanceKey)
	if err != nil {return nil, err}
  
	inst, found, err := ReadInstance(instanceKey)
	if err != nil || !found {return nil, err}
	
	return inst, nil
}
  

// StopSlaveNicely stops a slave such that SQL_thread and IO_thread are aligned (i.e.
// SQL_thread consumes all relay log entries)
func StopSlaveNicely(instanceKey *InstanceKey) (*Instance, error) {
	instance, err := ReadTopologyInstance(instanceKey)
	if err != nil {return instance, log.Errore(err)}
	
	if !instance.IsSlave() {
		return instance, errors.New(fmt.Sprintf("instance is not a slave: %+v", instanceKey))
	}
	
	_, err = ExecInstance(instanceKey, `stop slave io_thread`)
	
	for up_to_date := false; !up_to_date; {
		instance, err = ReadTopologyInstance(instanceKey)
		if err != nil {return instance, log.Errore(err)}
		
		if instance.SQLThreadUpToDate() {
			up_to_date = true
		} else {
			time.Sleep(200 * time.Millisecond)
		}
	}
	_, err = ExecInstance(instanceKey, `stop slave`)
	if err != nil {return instance, log.Errore(err)}
	
	instance, err = ReadTopologyInstance(instanceKey)
	return instance, err
}


// StopSlave stops replication on a given instance
func StopSlave(instanceKey *InstanceKey) (*Instance, error) {
	instance, err := ReadTopologyInstance(instanceKey)
	if err != nil {return instance, log.Errore(err)}
	
	if !instance.IsSlave() {
		return instance, errors.New(fmt.Sprintf("instance is not a slave: %+v", instanceKey))
	}
	_, err = ExecInstance(instanceKey, `stop slave`)
	if err != nil {return instance, log.Errore(err)}
	instance, err = ReadTopologyInstance(instanceKey)

	log.Infof("Stopped slave on %+v, Self:%+v, Exec:%+v", *instanceKey, instance.SelfBinlogCoordinates, instance.ExecBinlogCoordinates) 
	return instance, err
}


// StopSlave starts replication on a given instance
func StartSlave(instanceKey *InstanceKey) (*Instance, error) {
	instance, err := ReadTopologyInstance(instanceKey)
	if err != nil {return instance, log.Errore(err)}
	
	if !instance.IsSlave() {
		return instance, errors.New(fmt.Sprintf("instance is not a slave: %+v", instanceKey))
	}
	
	_, err = ExecInstance(instanceKey, `start slave`)
	if err != nil {return instance, log.Errore(err)}
	log.Infof("Started slave on %+v", instanceKey) 
	if config.Config.SlaveStartPostWaitMilliseconds > 0 {
		time.Sleep(time.Duration(config.Config.SlaveStartPostWaitMilliseconds) * time.Millisecond)
	}
	
	instance, err = ReadTopologyInstance(instanceKey)
	return instance, err
}


// StartSlaveUntilMasterCoordinates issuesa START SLAVE UNTIL... statement on given instance
func StartSlaveUntilMasterCoordinates(instanceKey *InstanceKey, masterCoordinates *BinlogCoordinates) (*Instance, error) {
	instance, err := ReadTopologyInstance(instanceKey)
	if err != nil {return instance, log.Errore(err)}
	
	if !instance.IsSlave() {
		return instance, errors.New(fmt.Sprintf("instance is not a slave: %+v", instanceKey))
	}
	if instance.SlaveRunning() {
		return instance, errors.New(fmt.Sprintf("slave already running: %+v", instanceKey))
	}

	log.Infof("Will start slave on %+v until coordinates: %+v", instanceKey, masterCoordinates) 

	_, err = ExecInstance(instanceKey, fmt.Sprintf("start slave until master_log_file='%s', master_log_pos=%d", 
		masterCoordinates.LogFile, masterCoordinates.LogPos))
	if err != nil {return instance, log.Errore(err)}
		
	for up_to_date := false; !up_to_date; {
		instance, err = ReadTopologyInstance(instanceKey)
		if err != nil {return instance, log.Errore(err)}
		
		switch {
			case instance.ExecBinlogCoordinates.SmallerThan(masterCoordinates): time.Sleep(200 * time.Millisecond)
			case instance.ExecBinlogCoordinates.Equals(masterCoordinates): up_to_date = true
			case masterCoordinates.SmallerThan(&instance.ExecBinlogCoordinates): return instance, errors.New(fmt.Sprintf("Start SLAVE UNTIL is past coordinates: %+v", instanceKey))
		}
	}
		
	instance, err = StopSlave(instanceKey)
	if err != nil {return instance, log.Errore(err)} 
	
	return instance, err
}


// ChangeMasterTo changes the given instance's master according to given input.
func ChangeMasterTo(instanceKey *InstanceKey, masterKey *InstanceKey, masterBinlogCoordinates *BinlogCoordinates) (*Instance, error) {
	instance, err := ReadTopologyInstance(instanceKey)
	if err != nil {return instance, log.Errore(err)}
	
	if instance.SlaveRunning() {
		return instance, errors.New(fmt.Sprintf("Cannot change master on: %+v because slave is running", instanceKey))
	}
	
	_, err = ExecInstance(instanceKey, fmt.Sprintf("change master to master_host='%s', master_port=%d, master_log_file='%s', master_log_pos=%d", 
		masterKey.Hostname, masterKey.Port, masterBinlogCoordinates.LogFile, masterBinlogCoordinates.LogPos))
	if err != nil {return instance, log.Errore(err)}
	log.Infof("Changed master on %+v to: %+v, %+v", instanceKey, masterKey, masterBinlogCoordinates) 
	
	instance, err = ReadTopologyInstance(instanceKey)
	return instance, err
}


// MasterPosWait issues a MASTER_POS_WAIT() an given instance according to given coordinates.
func MasterPosWait(instanceKey *InstanceKey, binlogCoordinates *BinlogCoordinates) (*Instance, error) {
	instance, err := ReadTopologyInstance(instanceKey)
	if err != nil {return instance, log.Errore(err)}
	
	_, err = ExecInstance(instanceKey, fmt.Sprintf("select master_pos_wait('%s', %d)", 
		binlogCoordinates.LogFile, binlogCoordinates.LogPos))
	if err != nil {return instance, log.Errore(err)}
	log.Infof("Instance %+v has reached coordinates: %+v", instanceKey, binlogCoordinates) 
	
	instance, err = ReadTopologyInstance(instanceKey)
	return instance, err
}
