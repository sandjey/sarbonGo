--
-- PostgreSQL database dump
--


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
-- Name: offers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.offers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    carrier_id uuid NOT NULL,
    price double precision NOT NULL,
    currency character varying NOT NULL,
    comment character varying,
    status character varying DEFAULT 'pending'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    rejection_reason text,
    proposed_by character varying(20) DEFAULT 'DRIVER'::character varying NOT NULL,
    proposed_by_id uuid,
    negotiation_dispatcher_id uuid,
    CONSTRAINT offers_proposed_by_check CHECK (((proposed_by)::text = ANY ((ARRAY['DRIVER'::character varying, 'DISPATCHER'::character varying, 'DRIVER_MANAGER'::character varying])::text[]))),
    CONSTRAINT offers_status_check CHECK (((status)::text = ANY ((ARRAY['PENDING'::character varying, 'ACCEPTED'::character varying, 'REJECTED'::character varying, 'WAITING_DRIVER_CONFIRM'::character varying, 'CANCELED'::character varying])::text[])))
);


--
-- Name: offers offers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offers
    ADD CONSTRAINT offers_pkey PRIMARY KEY (id);


--
-- Name: idx_offers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_offers_cargo_id ON public.offers USING btree (cargo_id);


--
-- Name: idx_offers_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_offers_status ON public.offers USING btree (status);


--
-- Name: offers offers_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offers
    ADD CONSTRAINT offers_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


