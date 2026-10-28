(* The percentile of service/src/main.rs, modelled in OCaml so that
   check.ml can test its invariants on many inputs. *)

(* Nearest rank: the value below which a fraction [q] (0.0 to 1.0) of
   the samples fall. *)
let percentile samples q =
  let sorted = List.sort compare samples in
  let n = List.length sorted in
  let rank = int_of_float (Float.ceil (q *. float_of_int n)) in
  List.nth sorted rank
