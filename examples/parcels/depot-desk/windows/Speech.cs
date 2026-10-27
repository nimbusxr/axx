// How the depot desk reads aloud in its Windows builds (../README.md): with SAPI, through
// System.Speech, in the user's default voice. One synthesizer for as long as the desk runs: what it
// says goes on after Say returns.
using System.Speech.Synthesis;

namespace DepotDesk;

static class Speech
{
    static SpeechSynthesizer? voice;

    public static void Say(string text)
    {
        try
        {
            voice ??= new SpeechSynthesizer();
            voice.SpeakAsync(text);
        }
        catch (Exception e) // no voice on this machine
        {
            Console.Error.WriteLine($"The desk cannot read aloud: {e.Message}");
            voice = null;
        }
    }
}
