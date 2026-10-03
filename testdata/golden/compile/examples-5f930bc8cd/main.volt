/* Ultrasonic proximity alert — HC-SR04 distance sensor + LED
 *
 * HC-SR04 uses two GPIO pins: TRIG (output pulse) and ECHO (input).
 * ECHO is a 5V line — feed it through a 10k/20k divider into a 3.3V MCU.
 */

hardware {
    pin led = 2
    device dist: HCSR04(trig: 26, echo: 27)
}

setup {
    log("boot", "ultrasonic ready")
}

every 200ms {
    var cm: float = dist.distance_cm()
    log("dist", "{cm} cm")

    // Light the LED when something is within 20 cm.
    if cm < 20.0 {
        led.on()
    } else {
        led.off()
    }
}
