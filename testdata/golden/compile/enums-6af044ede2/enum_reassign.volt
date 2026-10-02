
chip esp32
enum TrafficLight {
    red
    yellow
    green
}
setup {
    var light: TrafficLight = TrafficLight.red
    light = TrafficLight.green
}
