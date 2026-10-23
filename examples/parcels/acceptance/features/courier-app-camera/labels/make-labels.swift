// SPDX-License-Identifier: Apache-2.0

// Writes the parcel labels the couriers' iPhone camera sees in ../courier-app-camera-ios.feature,
// as a phone held over a parcel photographs them: a label on cardboard, its QR code above the
// parcel's reference printed large. On a Mac:
//
//	swift make-labels.swift [directory] [reference with a QR code] [reference of a torn label]
//
//   - label-<reference>.png: the label, whole;
//   - torn-label-<reference>.mp4: a label whose code is torn off, the reference left, as the
//     courier lifts the parcel into view: 3 seconds, 30 frames a second.
import AVFoundation
import AppKit
import CoreImage
import CoreImage.CIFilterBuiltins

let args = CommandLine.arguments
let dir = URL(fileURLWithPath: args.count > 1 ? args[1] : ".")
let scanned = args.count > 2 ? args[2] : "PX-IOS-9621"
let torn = args.count > 3 ? args[3] : "PX-IOS-9622"

// The photo: 1080 by 1920, upright, as the phone held over the parcel sees it.
let size = CGSize(width: 1080, height: 1920)

func qr(_ text: String) -> CGImage {
    let f = CIFilter.qrCodeGenerator()
    f.message = Data(text.utf8)
    f.correctionLevel = "M"
    let img = f.outputImage!.transformed(by: CGAffineTransform(scaleX: 16, y: 16))
    return CIContext().createCGImage(img, from: img.extent)!
}

// A label on cardboard: lifted is how far up the parcel is, 0 to 1 (1: in the middle of the
// picture); withCode, whether its code is there.
func label(_ reference: String, withCode: Bool, lifted: CGFloat = 1) -> CGImage {
    let ctx = CGContext(data: nil, width: Int(size.width), height: Int(size.height), bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue)!
    // The porch's boards, then the parcel's cardboard.
    ctx.setFillColor(CGColor(red: 0.42, green: 0.3, blue: 0.2, alpha: 1))
    ctx.fill(CGRect(origin: .zero, size: size))
    let drop = (1 - lifted) * size.height
    ctx.setFillColor(CGColor(red: 0.7, green: 0.52, blue: 0.32, alpha: 1))
    ctx.fill(CGRect(x: 60, y: 180 - drop, width: size.width - 120, height: size.height - 360))
    // The label.
    let paper = CGRect(x: 160, y: 420 - drop, width: size.width - 320, height: 1080)
    ctx.setFillColor(.white)
    ctx.fill(paper)
    let ns = NSGraphicsContext(cgContext: ctx, flipped: false)
    NSGraphicsContext.current = ns
    func text(_ s: String, _ font: NSFont, at y: CGFloat) {
        let a = NSAttributedString(string: s, attributes: [.font: font, .foregroundColor: NSColor.black])
        a.draw(at: CGPoint(x: paper.minX + 50, y: y))
    }
    text("PARCELS", .boldSystemFont(ofSize: 56), at: paper.maxY - 110)
    text("STANDARD", .systemFont(ofSize: 40), at: paper.maxY - 170)
    if withCode {
        ctx.draw(qr(reference), in: CGRect(x: paper.midX - 230, y: paper.minY + 300, width: 460, height: 460))
    } else {
        // Torn off: the cardboard shows where the code was.
        ctx.setFillColor(CGColor(red: 0.7, green: 0.52, blue: 0.32, alpha: 1))
        ctx.move(to: CGPoint(x: paper.minX, y: paper.minY + 790))
        ctx.addLine(to: CGPoint(x: paper.maxX, y: paper.minY + 760))
        ctx.addLine(to: CGPoint(x: paper.maxX, y: paper.minY + 290))
        ctx.addLine(to: CGPoint(x: paper.minX + 300, y: paper.minY + 320))
        ctx.addLine(to: CGPoint(x: paper.minX, y: paper.minY + 280))
        ctx.fillPath()
    }
    text(reference, .monospacedSystemFont(ofSize: 96, weight: .bold), at: paper.minY + 150)
    text("Kochstr. 32, 04275 Leipzig", .systemFont(ofSize: 40), at: paper.minY + 70)
    NSGraphicsContext.current = nil
    return ctx.makeImage()!
}

func writePNG(_ img: CGImage, _ name: String) throws {
    let rep = NSBitmapImageRep(cgImage: img)
    try rep.representation(using: .png, properties: [:])!.write(to: dir.appendingPathComponent(name))
}

func writeVideo(_ name: String, frames: Int, _ frame: (Int) -> CGImage) throws {
    let url = dir.appendingPathComponent(name)
    try? FileManager.default.removeItem(at: url)
    let w = try AVAssetWriter(outputURL: url, fileType: .mp4)
    let input = AVAssetWriterInput(mediaType: .video, outputSettings: [
        AVVideoCodecKey: AVVideoCodecType.h264, AVVideoWidthKey: Int(size.width), AVVideoHeightKey: Int(size.height),
        AVVideoCompressionPropertiesKey: [AVVideoAverageBitRateKey: 2_000_000],
    ])
    let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [
        kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32ARGB, kCVPixelBufferWidthKey as String: Int(size.width),
        kCVPixelBufferHeightKey as String: Int(size.height),
    ])
    w.add(input)
    w.startWriting()
    w.startSession(atSourceTime: .zero)
    for i in 0..<frames {
        while !input.isReadyForMoreMediaData { usleep(1000) }
        var pb: CVPixelBuffer?
        CVPixelBufferPoolCreatePixelBuffer(nil, adaptor.pixelBufferPool!, &pb)
        CVPixelBufferLockBaseAddress(pb!, [])
        let ctx = CGContext(data: CVPixelBufferGetBaseAddress(pb!), width: Int(size.width), height: Int(size.height), bitsPerComponent: 8,
                            bytesPerRow: CVPixelBufferGetBytesPerRow(pb!), space: CGColorSpaceCreateDeviceRGB(),
                            bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue)!
        ctx.draw(frame(i), in: CGRect(origin: .zero, size: size))
        CVPixelBufferUnlockBaseAddress(pb!, [])
        adaptor.append(pb!, withPresentationTime: CMTime(value: CMTimeValue(i), timescale: 30))
    }
    input.markAsFinished()
    let done = DispatchSemaphore(value: 0)
    w.finishWriting { done.signal() }
    done.wait()
    if w.status != .completed { throw w.error ?? NSError(domain: "make-labels", code: 1) }
}

try writePNG(label(scanned, withCode: true), "label-\(scanned).png")
// The parcel comes up into view in the first second, then the courier holds it still.
try writeVideo("torn-label-\(torn).mp4", frames: 90) { i in
    let t = min(1, CGFloat(i) / 30)
    return label(torn, withCode: false, lifted: t * t * (3 - 2 * t))
}
print("wrote label-\(scanned).png and torn-label-\(torn).mp4 to \(dir.path)")
