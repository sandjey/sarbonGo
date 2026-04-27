--
-- PostgreSQL database dump
--

\restrict b76IUboISRs9OmzsIr0H1SugyfjB0hUjQUj0fuscj2Afg3MCqQqUSZfc8roPHsq

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: payments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    is_negotiable boolean DEFAULT false NOT NULL,
    price_request boolean DEFAULT false NOT NULL,
    total_amount double precision,
    total_currency character varying,
    with_prepayment boolean DEFAULT false NOT NULL,
    without_prepayment boolean DEFAULT true NOT NULL,
    prepayment_amount double precision,
    prepayment_currency character varying,
    prepayment_type character varying,
    remaining_amount double precision,
    remaining_currency character varying,
    remaining_type character varying,
    payment_note character varying(500),
    payment_terms_note text
);


--
-- Name: payments payments_cargo_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_cargo_id_key UNIQUE (cargo_id);


--
-- Name: payments payments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_pkey PRIMARY KEY (id);


--
-- Name: idx_payments_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_payments_cargo_id ON public.payments USING btree (cargo_id);


--
-- Name: payments payments_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict b76IUboISRs9OmzsIr0H1SugyfjB0hUjQUj0fuscj2Afg3MCqQqUSZfc8roPHsq

