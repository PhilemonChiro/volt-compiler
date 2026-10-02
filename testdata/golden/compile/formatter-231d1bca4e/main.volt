
chip esp32
hardware{pin led:Output=2}
setup{
var x:int=1+2
var y:float=3.14
if x>0{led.on()}
}
