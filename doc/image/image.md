前端                      路由                  JWT         controller              Service            common
上传界面                /image/recognize                  RecognizeImage         RecognizeImage        ONNX推理
ImageRecognition.vue -> image.go        -> jwt.go   -> image_controller.go -> image_service.go  -> image_recoginize.go

    外部服务
-> ONNX Runtime

● 前端：ImageRecognition.vue上传图片文件，通过FormData发送。
● 路由层：Image.go路由匹配/image/recognize路径。
● 中间件层：jwt.go验证用户身份。
● 控制器层：image.go控制器（RecognizeImage）解析multipart文件。
● 服务层：image.go服务调用识别器，处理文件读取。
● 通用组件：ImageRecognizer封装ONNX推理，加载MobileNetV2模型和ImageNet标签。
● 外部服务：ONNX Runtime执行本地模型推理，返回类别概率。
流程包括图像解码、缩放归一化、推理和结果映射。无DAO层，直接返回识别结果