
chip esp32
hardware{pin led:Output=2}
machine blinker{
state off{
enter{led.off()}
after 1s->on
}
state on{
enter{led.on()}
after 500ms->off
}
}
