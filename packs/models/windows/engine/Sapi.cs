using System;
using System.Runtime.InteropServices;

// SAPI 5.4's engine interfaces and text fragments (sapiddk.h), as an engine implements them; the
// engine site's Write it calls through the site's vtable (AxxVoice.Write).
namespace Axx.Sapi;

[ComImport, Guid("5B559F40-E952-11D2-BB91-00C04F8EE6C0"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface ISpObjectWithToken
{
    void SetObjectToken(IntPtr token);
    void GetObjectToken(out IntPtr token);
}

[ComImport, Guid("A74D7C8E-4CC5-4F2F-A6EB-804DEE18500E"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface ISpTTSEngine
{
    void Speak(uint speakFlags, ref Guid formatId, IntPtr waveFormatEx, IntPtr textFragList, IntPtr site);
    void GetOutputFormat(IntPtr targetFormatId, IntPtr targetWaveFormatEx, out Guid outputFormatId, out IntPtr coMemOutputWaveFormatEx);
}

[StructLayout(LayoutKind.Sequential)]
public struct SPVPITCH { public int MiddleAdj; public int RangeAdj; }

[StructLayout(LayoutKind.Sequential)]
public struct SPVCONTEXT { public IntPtr pCategory; public IntPtr pBefore; public IntPtr pAfter; }

[StructLayout(LayoutKind.Sequential)]
public struct SPVSTATE
{
    public int eAction;
    public ushort LangID;
    public ushort wReserved;
    public int EmphAdj;
    public int RateAdj;
    public uint Volume;
    public SPVPITCH PitchAdj;
    public uint SilenceMSecs;
    public IntPtr pPhoneIds;
    public int ePartOfSpeech;
    public SPVCONTEXT Context;
}

[StructLayout(LayoutKind.Sequential)]
public struct SPVTEXTFRAG
{
    public IntPtr pNext;
    public SPVSTATE State;
    public IntPtr pTextStart;
    public uint ulTextLen;
    public uint ulTextSrcOffset;
}
