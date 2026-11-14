import SwiftUI

@main
struct MavaApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            Group {
                if model.onboarded { ChatsView() } else { OnboardingView() }
            }
            .environmentObject(model)
            .fullScreenCover(isPresented: Binding(get: { model.call != nil }, set: { if !$0 { model.dismissCall() } })) {
                CallView().environmentObject(model)
            }
            .alert("Something went wrong", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(model.error ?? "")
            }
        }
    }
}
