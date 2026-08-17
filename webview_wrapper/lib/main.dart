import 'package:flutter/material.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:webview_flutter/webview_flutter.dart';
import 'package:webview_flutter_android/webview_flutter_android.dart';

// 已部署、已實測的城市之眼網頁。改這裡就能換成別的網址，不用動其他程式碼。
const String kAppUrl =
    'https://hackathon-smartcity-411737108721.asia-east1.run.app';

void main() {
  runApp(const CityEyeApp());
}

class CityEyeApp extends StatelessWidget {
  const CityEyeApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      title: '城市之眼',
      theme: ThemeData(useMaterial3: true, colorSchemeSeed: Colors.blue),
      home: const WebViewScreen(),
    );
  }
}

class WebViewScreen extends StatefulWidget {
  const WebViewScreen({super.key});

  @override
  State<WebViewScreen> createState() => _WebViewScreenState();
}

class _WebViewScreenState extends State<WebViewScreen> {
  late final WebViewController _controller;
  bool _ready = false;

  @override
  void initState() {
    super.initState();
    _bootstrap();
  }

  // 相機/麥克風要先在 OS 層要到權限，WebView 內的 getUserMedia() 才有機會成功；
  // 光靠 AndroidManifest 宣告不夠，Android 6+ 一定要另外跑 runtime request。
  Future<void> _bootstrap() async {
    await [Permission.camera, Permission.microphone].request();

    final controller = WebViewController();
    await controller.setJavaScriptMode(JavaScriptMode.unrestricted);
    await controller.setBackgroundColor(const Color(0xFF000000));

    // WebView 本身也有自己的一層「網頁要用相機/麥克風」授權請求，
    // 要自己接起來自動同意，不然網頁裡的 getUserMedia() 會靜默失敗。
    final androidController = controller.platform as AndroidWebViewController;
    await androidController.setMediaPlaybackRequiresUserGesture(false);
    await androidController.setOnPlatformPermissionRequest(
      (PlatformWebViewPermissionRequest request) {
        request.grant();
      },
    );

    await controller.loadRequest(Uri.parse(kAppUrl));

    if (!mounted) return;
    setState(() {
      _controller = controller;
      _ready = true;
    });
  }

  @override
  Widget build(BuildContext context) {
    if (!_ready) {
      return const Scaffold(
        backgroundColor: Colors.black,
        body: Center(child: CircularProgressIndicator()),
      );
    }
    return Scaffold(
      backgroundColor: Colors.black,
      body: SafeArea(child: WebViewWidget(controller: _controller)),
    );
  }
}
