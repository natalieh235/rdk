//go:build linux

// Package customlinux implements a board running linux
package customlinux

import (
	"context"
	"encoding/json"
	"os"
	"strconv"

	"github.com/edaniels/golog"
	"periph.io/x/host/v3"

	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/components/board/genericlinux"
	"go.viam.com/rdk/resource"
)

func init() {
	if _, err := host.Init(); err != nil {
		golog.Global().Debugw("error initializing host", "error", err)
	}

	RegisterCustomBoard("customlinux")
}

// customLinuxBoard wraps the genericlinux board type so that both can implement their own Reconfigure function
type customLinuxBoard struct {
	*genericlinux.SysfsBoard
}

// RegisterCustomBoard registers a sysfs based board using the pin mappings.
func RegisterCustomBoard(modelName string) {
	resource.RegisterComponent(
		board.API,
		resource.DefaultModelFamily.WithModel(modelName),
		resource.Registration[board.Board, *Config]{
			Constructor: func(
				ctx context.Context,
				_ resource.Dependencies,
				conf resource.Config,
				logger golog.Logger,
			) (board.Board, error) {
				newConf, err := resource.NativeConfig[*Config](conf)
				if err != nil {
					return nil, err
				}

				pinDefs, err := parseBoardConfig(newConf.PinConfigFilePath)
				if err != nil {
					return nil, err
				}

				gpioMappings, err := genericlinux.GetGPIOBoardMappingFromPinDefs(pinDefs)
				if err != nil {
					return nil, err
				}

				b, err := genericlinux.NewBoard(ctx, conf.ResourceName().AsNamed(), &newConf.Config, gpioMappings, false, logger)
				if err != nil {
					return nil, err
				}

				// gb, ok := genericlinux.SysfsBoard.(b)
				return &customLinuxBoard{b}, nil
			},
		})
}

func parseBoardConfig(filePath string) ([]genericlinux.PinDefinition, error) {
	pinData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var parsedPinData GenericLinuxPins
	if err := json.Unmarshal(pinData, &parsedPinData); err != nil {
		return nil, err
	}

	pinDefs := make([]genericlinux.PinDefinition, len(parsedPinData.Pins))
	for i, pin := range parsedPinData.Pins {
		err = pin.Validate(filePath)
		if err != nil {
			return nil, err
		}

		pinName, err := strconv.Atoi(pin.Name)
		if err != nil {
			return nil, err
		}

		pinDefs[i] = genericlinux.PinDefinition{
			GPIOChipRelativeIDs: map[int]int{pin.Ngpio: pin.RelativeID}, // ngpio: relative id map
			PinNumberBoard:      pinName,
			PWMChipSysFSDir:     pin.PWMChipSysFSDir,
			PWMID:               pin.PWMID,
		}
	}

	return pinDefs, nil
}

// Reconfigure reconfigures the board with interrupt pins, spi and i2c, and analogs.
func (b *customLinuxBoard) Reconfigure(
	ctx context.Context,
	_ resource.Dependencies,
	conf resource.Config,
) error {
	newConf, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	return b.ReconfigureParsedConfig(ctx, &newConf.Config)
}
