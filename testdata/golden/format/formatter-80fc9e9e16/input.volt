
chip esp32
hardware{
pin led:Output=2
}
task blink{
every 500ms{
var x:int=1
led.toggle()
log("app","tick")
}
}
