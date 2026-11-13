package parcels;

public class Desk {
    static String ship(int n) {
        if (n > 0) {
            return "shipped";
        }
        return "held";
    }

    static int close() {
        return 1;
    }

    public static void main(String[] args) {
        System.out.println(ship(args.length + 1));
    }
}
