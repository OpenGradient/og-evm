package cli

import (
	"github.com/spf13/cobra"

	"github.com/cosmos/evm/x/svip/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewTxCmd returns a root CLI command handler for svip transaction commands.
func NewTxCmd() *cobra.Command {
	txCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "svip subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	txCmd.AddCommand(
		NewFundPoolCmd(),
	)
	return txCmd
}

// NewFundPoolCmd returns a CLI command handler for funding the SVIP reward pool.
func NewFundPoolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fund-pool COINS",
		Short: "Fund the SVIP reward pool",
		Long:  "Fund the SVIP reward pool with the specified coins. Example: ogd tx svip fund-pool 1000000000000000000000ogwei --from dev0",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			coins, err := sdk.ParseCoinsNormalized(args[0])
			if err != nil {
				return err
			}

			msg := &types.MsgFundPool{
				Depositor: cliCtx.GetFromAddress().String(),
				Amount:    coins,
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
