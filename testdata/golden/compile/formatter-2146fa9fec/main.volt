
chip esp32
hardware{pin led:Output=2}
setup{
for i in 0..10 step 2{led.toggle()}
}
