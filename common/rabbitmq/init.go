package rabbitmq

var RMQMessage *RabbitMQ

func InitRabbitMQ() {
	// 创建MQ & 启动消费者
	RMQMessage = NewWorkRabbitMQ("Message")
	go RMQMessage.Consume(MQMessage)
}

// DestroyRabbitMQ 销毁RabbitMQ
func DestroyRabbitMQ() {
	RMQMessage.Destroy()
}
