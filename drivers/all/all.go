// Package all подключает все драйверы устройств: импортируйте его ради побочного
// эффекта — регистрации драйверов в реестре drivers.
package all

import (
	_ "github.com/fess932/homeLab/drivers/httpjson"
	_ "github.com/fess932/homeLab/drivers/tuya"
)
