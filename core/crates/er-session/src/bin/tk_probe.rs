fn main() {
    let secret = b"dev-secret-change-me";
    let exp = 2000000000i64;
    let tk = er_session::ticket::issue(secret, "ses-e2e-1", "devA", "devB", exp);
    println!("TICKET={}", tk);
    let body = er_session::wire::encode_register("devA", &tk);
    let frame = er_session::wire::encode_frame(er_session::wire::FRAME_REGISTER, "ses-e2e-1", &body);
    println!("FRAMEHEX={}", frame.iter().map(|b| format!("{:02x}", b)).collect::<String>());
}
