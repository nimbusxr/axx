using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using Axx.Sapi;

namespace Axx;

// axx's voice for SAPI (models-windows): it speaks each text with Piper (AXX_PIPER) and the Piper
// voice stub in the app's axx-models folder (AXX_MODELS_DIR) into SAPI's output, and puts each
// utterance, whole, in axx-models\.speaker, which the speaker step hears.
[ComVisible(true), Guid("3C5E8D2A-7B41-4E9F-A6C3-9D0B1F2E4A57"), ClassInterface(ClassInterfaceType.None)]
public sealed class AxxVoice : ISpTTSEngine, ISpObjectWithToken
{
    static readonly Guid SPDFID_WaveFormatEx = new("C31ADBAE-527F-4FF5-A230-F62BB61FF70C");
    IntPtr token;

    public void SetObjectToken(IntPtr t)
    {
        if (t != IntPtr.Zero) Marshal.AddRef(t);
        token = t;
    }

    public void GetObjectToken(out IntPtr t)
    {
        if (token != IntPtr.Zero) Marshal.AddRef(token);
        t = token;
    }

    static string Models => Environment.GetEnvironmentVariable("AXX_MODELS_DIR") ?? "";

    // The voice stub (its .onnx beside its .onnx.json) and its sample rate.
    static (string voice, int rate) Voice()
    {
        foreach (var onnx in Directory.Exists(Models) ? Directory.GetFiles(Models, "*.onnx") : [])
        {
            if (!File.Exists(onnx + ".json")) continue;
            using var config = JsonDocument.Parse(File.ReadAllText(onnx + ".json"));
            return (onnx, config.RootElement.GetProperty("audio").GetProperty("sample_rate").GetInt32());
        }
        return ("", 16000);
    }

    public void GetOutputFormat(IntPtr targetFormatId, IntPtr targetWaveFormatEx, out Guid outputFormatId, out IntPtr coMemOutputWaveFormatEx)
    {
        // 16-bit mono PCM, at the voice's rate: a WAVEFORMATEX.
        var rate = Voice().rate;
        var w = Marshal.AllocCoTaskMem(18);
        Marshal.WriteInt16(w, 0, 1);
        Marshal.WriteInt16(w, 2, 1);
        Marshal.WriteInt32(w, 4, rate);
        Marshal.WriteInt32(w, 8, rate * 2);
        Marshal.WriteInt16(w, 12, 2);
        Marshal.WriteInt16(w, 14, 16);
        Marshal.WriteInt16(w, 16, 0);
        outputFormatId = SPDFID_WaveFormatEx;
        coMemOutputWaveFormatEx = w;
    }

    public void Speak(uint speakFlags, ref Guid formatId, IntPtr waveFormatEx, IntPtr textFragList, IntPtr site)
    {
        var text = new StringBuilder();
        for (var p = textFragList; p != IntPtr.Zero;)
        {
            var frag = Marshal.PtrToStructure<SPVTEXTFRAG>(p);
            if (frag.State.eAction is 0 or 5) // SPVA_Speak, SPVA_SpellOut
                text.Append(Marshal.PtrToStringUni(frag.pTextStart, (int)frag.ulTextLen));
            p = frag.pNext;
        }
        var (voice, _) = Voice();
        var piper = Environment.GetEnvironmentVariable("AXX_PIPER");
        if (voice == "" || string.IsNullOrEmpty(piper)) return;
        var speaker = Path.Combine(Models, ".speaker");
        var partial = Path.Combine(speaker, ".partial");
        Directory.CreateDirectory(partial);
        var name = $"{DateTime.UtcNow.Ticks:D20}-{Guid.NewGuid():N}.wav";
        var file = Path.Combine(partial, name);
        var start = new ProcessStartInfo(piper, ["--model", voice, "--output_file", file])
        {
            RedirectStandardInput = true, RedirectStandardOutput = true, RedirectStandardError = true,
            UseShellExecute = false, CreateNoWindow = true,
        };
        // Piper's output (the file's name) and its log are its own, not the app's.
        using (var run = Process.Start(start)!)
        {
            run.StandardInput.Write(text.ToString());
            run.StandardInput.Close();
            var output = run.StandardOutput.ReadToEndAsync();
            run.StandardError.ReadToEnd();
            output.Wait();
            run.WaitForExit();
        }
        var wav = File.ReadAllBytes(file);
        File.Move(file, Path.Combine(speaker, name));
        // The WAV's samples, its data chunk's, to SAPI.
        var (at, length) = Samples(wav);
        var pcm = Marshal.AllocHGlobal(length);
        try
        {
            Marshal.Copy(wav, at, pcm, length);
            Write(site, pcm, (uint)length);
        }
        finally { Marshal.FreeHGlobal(pcm); }
    }

    // Samples is where a WAV's data chunk is, after RIFF's header and the chunks before it.
    static (int at, int length) Samples(byte[] wav)
    {
        for (var at = 12; at + 8 <= wav.Length;)
        {
            var size = BitConverter.ToInt32(wav, at + 4);
            if (Encoding.ASCII.GetString(wav, at, 4) == "data")
                return (at + 8, Math.Min(size, wav.Length - at - 8));
            at += 8 + size + (size & 1);
        }
        return (0, 0);
    }

    // ISpTTSEngineSite::Write, through the site's vtable (after IUnknown's three, AddEvents,
    // GetEventInterest and GetActions): the site may be a managed object's (System.Speech's), which
    // no cast to this assembly's ISpTTSEngineSite reaches.
    static unsafe void Write(IntPtr site, IntPtr data, uint count)
    {
        var vtable = *(IntPtr**)site;
        var write = (delegate* unmanaged[Stdcall]<IntPtr, IntPtr, uint, uint*, int>)vtable[6];
        uint written;
        Marshal.ThrowExceptionForHR(write(site, data, count, &written));
    }
}
