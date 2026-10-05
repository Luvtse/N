import UIKit
import Flutter
import Firebase
import GoogleMaps

@UIApplicationMain
@objc class AppDelegate: FlutterAppDelegate {
    override func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
    ) -> Bool {
        // Initialize Firebase
        FirebaseApp.configure()
        
        // Configure Google Maps
        if let mapsApiKey = Bundle.main.object(forInfoDictionaryKey: "GMSApiKey") as? String {
            GMSServices.provideAPIKey(mapsApiKey)
        }
        
        // Configure notification delegate
        if #available(iOS 10.0, *) {
            UNUserNotificationCenter.current().delegate = self as UNUserNotificationCenterDelegate
        }
        
        // Register for remote notifications
        let authOptions: UNAuthorizationOptions = [.alert, .badge, .sound]
        UNUserNotificationCenter.current().requestAuthorization(
            options: authOptions,
            completionHandler: { _, _ in }
        )
        
        application.registerForRemoteNotifications()
        
        GeneratedPluginRegistrant.register(with: self)
        
        return super.application(application, didFinishLaunchingWithOptions: launchOptions)
    }
    
    // Handle remote notifications
    override func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        // Forward token to Firebase
        Messaging.messaging().apnsToken = deviceToken
    }
    
    // Handle notification display
    override func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .badge, .sound])
    }
    
    // Handle notification tap
    override func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let userInfo = response.notification.request.content.userInfo
        
        // Handle deep link if present
        if let deepLink = userInfo["deep_link"] as? String {
            handleDeepLink(deepLink)
        }
        
        completionHandler()
    }
    
    // Handle deep links
    private func handleDeepLink(_ deepLink: String) {
        if let url = URL(string: deepLink) {
            // Forward to Flutter via method channel
            let controller = window?.rootViewController as! FlutterViewController
            let channel = FlutterMethodChannel(
                name: "com.nidaw.app/deeplink",
                binaryMessenger: controller.binaryMessenger
            )
            channel.invokeMethod("handleDeepLink", arguments: url.absoluteString)
        }
    }
    
    // Handle URL schemes
    override func application(
        _ app: UIApplication,
        open url: URL,
        options: [UIApplication.OpenURLOptionsKey: Any] = [:]
    ) -> Bool {
        return true
    }
}