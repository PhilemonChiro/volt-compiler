
chip esp32
hardware {
    pin heater: Output = 2
    pin fan: Output = 4
    bus i2c0: I2C(sda: 21, scl: 22)
    device bme: BME280 on i2c0 at 0x76
}

storage target_temp: int = 22

machine thermostat {
    state idle {
        enter {
            heater.off()
            fan.off()
        }
        after 5s -> heating
    }
    state heating {
        enter { heater.on() }
        exit { heater.off() }
        after 30s -> cooling
    }
    state cooling {
        enter { fan.on() }
        exit { fan.off() }
        after 30s -> idle
    }
}

task sensor_reader {
    every 2s {
        var temp: float = bme.temperature()
    }
}
