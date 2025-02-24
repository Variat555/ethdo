// Copyright © 2021 Weald Technology Trading.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validatorexpectation

import (
	"context"
	"fmt"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/pkg/errors"
	standardchaintime "github.com/wealdtech/ethdo/services/chaintime/standard"
	"github.com/wealdtech/ethdo/util"
)

func (c *command) process(ctx context.Context) error {
	// Obtain information we need to process.
	if err := c.setup(ctx); err != nil {
		return err
	}

	if c.debug {
		fmt.Printf("Active validators: %d\n", c.activeValidators)
	}

	if err := c.calculateProposalChance(ctx); err != nil {
		return err
	}

	return c.calculateSyncCommitteeChance(ctx)
}

func (c *command) calculateProposalChance(ctx context.Context) error {
	// Chance of proposing a block is 1/activeValidators.
	// Expectation of number of slots before proposing a block is 1/p, == activeValidators slots.

	spec, err := c.eth2Client.(eth2client.SpecProvider).Spec(ctx)
	if err != nil {
		return err
	}

	tmp, exists := spec["SECONDS_PER_SLOT"]
	if !exists {
		return errors.New("spec missing SECONDS_PER_SLOT")
	}
	slotDuration, isType := tmp.(time.Duration)
	if !isType {
		return errors.New("SECONDS_PER_SLOT of incorrect type")
	}

	c.timeBetweenProposals = slotDuration * time.Duration(c.activeValidators) / time.Duration(c.validators)

	return nil
}

func (c *command) calculateSyncCommitteeChance(ctx context.Context) error {
	// Chance of being in a sync committee is SYNC_COMMITTEE_SIZE/activeValidators.
	// Expectation of number of periods before being in a sync committee is 1/p, activeValidators/SYNC_COMMITTEE_SIZE periods.

	spec, err := c.eth2Client.(eth2client.SpecProvider).Spec(ctx)
	if err != nil {
		return err
	}

	tmp, exists := spec["SECONDS_PER_SLOT"]
	if !exists {
		return errors.New("spec missing SECONDS_PER_SLOT")
	}
	slotDuration, isType := tmp.(time.Duration)
	if !isType {
		return errors.New("SECONDS_PER_SLOT of incorrect type")
	}

	tmp, exists = spec["SYNC_COMMITTEE_SIZE"]
	if !exists {
		return errors.New("spec missing SYNC_COMMITTEE_SIZE")
	}
	syncCommitteeSize, isType := tmp.(uint64)
	if !isType {
		return errors.New("SYNC_COMMITTEE_SIZE of incorrect type")
	}

	tmp, exists = spec["SLOTS_PER_EPOCH"]
	if !exists {
		return errors.New("spec missing SLOTS_PER_EPOCH")
	}
	slotsPerEpoch, isType := tmp.(uint64)
	if !isType {
		return errors.New("SLOTS_PER_EPOCH of incorrect type")
	}

	tmp, exists = spec["EPOCHS_PER_SYNC_COMMITTEE_PERIOD"]
	if !exists {
		return errors.New("spec missing EPOCHS_PER_SYNC_COMMITTEE_PERIOD")
	}
	epochsPerPeriod, isType := tmp.(uint64)
	if !isType {
		return errors.New("EPOCHS_PER_SYNC_COMMITTEE_PERIOD of incorrect type")
	}

	periodsBetweenSyncCommittees := uint64(c.activeValidators) / syncCommitteeSize
	if c.debug {
		fmt.Printf("Sync committee periods between inclusion: %d\n", periodsBetweenSyncCommittees)
	}

	c.timeBetweenSyncCommittees = slotDuration * time.Duration(slotsPerEpoch*epochsPerPeriod) * time.Duration(periodsBetweenSyncCommittees) / time.Duration(c.validators)

	return nil
}

func (c *command) setup(ctx context.Context) error {
	var err error

	// Connect to the client.
	c.eth2Client, err = util.ConnectToBeaconNode(ctx, c.connection, c.timeout, c.allowInsecureConnections)
	if err != nil {
		return errors.Wrap(err, "failed to connect to beacon node")
	}

	chainTime, err := standardchaintime.New(ctx,
		standardchaintime.WithSpecProvider(c.eth2Client.(eth2client.SpecProvider)),
		standardchaintime.WithForkScheduleProvider(c.eth2Client.(eth2client.ForkScheduleProvider)),
		standardchaintime.WithGenesisTimeProvider(c.eth2Client.(eth2client.GenesisTimeProvider)),
	)
	if err != nil {
		return errors.Wrap(err, "failed to set up chaintime service")
	}

	// Obtain the number of active validators.
	var isProvider bool
	c.validatorsProvider, isProvider = c.eth2Client.(eth2client.ValidatorsProvider)
	if !isProvider {
		return errors.New("connection does not provide validator information")
	}

	validators, err := c.validatorsProvider.Validators(ctx, "head", nil)
	if err != nil {
		return errors.Wrap(err, "failed to obtain validators")
	}

	currentEpoch := chainTime.CurrentEpoch()
	for _, validator := range validators {
		if validator.Validator.ActivationEpoch <= currentEpoch &&
			validator.Validator.ExitEpoch > currentEpoch {
			c.activeValidators++
		}
	}

	return nil
}
